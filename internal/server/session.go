package server

import (
	"context"
	"sync"
	"time"

	"github.com/iOliverNguyen/loggi/internal/filter"
	"github.com/iOliverNguyen/loggi/internal/store"
	"github.com/iOliverNguyen/loggi/internal/wire"
)

// Conn abstracts a duplex connection (unix socket or websocket).
type Conn interface {
	Read(v *wire.ClientMsg) error
	Write(v *wire.ServerMsg) error
	Close() error
}

// session holds per-connection state.
type session struct {
	id     uint64
	srv    *Server
	conn   Conn
	subs   map[uint64]*sessionSub
	subsMu sync.Mutex
	// stdin sources owned by this session (closed when session ends).
	stdinSrcs map[uint64]*stdinSource
}

type sessionSub struct {
	subID    uint64
	storeID  uint64    // store.Subscriber.ID
	node     filter.Node // parsed filter, kept for re-planning on history/filter-update
	historyN int       // history depth requested at subscribe time
}

var sessionGen sessionIDGen

type sessionIDGen struct {
	mu sync.Mutex
	n  uint64
}

func (g *sessionIDGen) next() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.n++
	return g.n
}

// runSession reads frames until the connection closes; dispatches to handlers.
func (s *Server) runSession(ctx context.Context, conn Conn) {
	id := sessionGen.next()
	s.sessionCnt.Add(1)
	s.cancelIdle()
	defer func() {
		s.sessionCnt.Add(-1)
		_ = conn.Close()
		s.maybeStartIdle()
	}()

	sess := &session{
		id:        id,
		srv:       s,
		conn:      conn,
		subs:      make(map[uint64]*sessionSub),
		stdinSrcs: make(map[uint64]*stdinSource),
	}
	// Send the snapshot BEFORE registering the session. If we registered
	// first, a concurrent broadcastSourceState (e.g. observeMode publishing
	// "open" on first ingest) could slip a "source" frame in front of the
	// snapshot — clients consume the first frame as a snapshot.
	//
	// Note: the snapshot is intentionally config-only (Sources + current
	// store Head). Row data flows through per-subscriber channels created
	// after registerSession; clients page in older rows via History RPC.
	// Any rows appended between this Write and registerSession are picked
	// up by the subscription's normal forward-fill — no replay gap.
	_ = conn.Write(&wire.ServerMsg{
		Type: wire.SMsgSnapshot,
		Snapshot: &wire.Snapshot{
			Sources: snapshotSources(s.Sources()),
			Head:    s.store.Head(),
		},
	})

	s.registerSession(sess)
	defer s.unregisterSession(id)
	defer sess.cleanup()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		var msg wire.ClientMsg
		if err := conn.Read(&msg); err != nil {
			return
		}
		sess.handle(&msg)
	}
}

func snapshotSources(in []SourceInfo) []wire.SourceEvent {
	out := make([]wire.SourceEvent, len(in))
	for i, s := range in {
		out[i] = wire.SourceEvent{
			SourceID: s.ID, Kind: s.Kind, Name: s.Name, Mode: s.Mode, State: s.State,
		}
	}
	return out
}

func (sess *session) cleanup() {
	sess.subsMu.Lock()
	for _, sub := range sess.subs {
		sess.srv.store.Unsubscribe(sub.storeID)
	}
	sess.subs = nil
	sess.subsMu.Unlock()
	for _, src := range sess.stdinSrcs {
		_ = src.Close()
	}
}

func (sess *session) handle(msg *wire.ClientMsg) {
	switch msg.Type {
	case wire.CMsgSubscribe:
		sess.handleSubscribe(msg)
	case wire.CMsgUnsubscribe:
		if msg.Unsubscribe != nil {
			sess.unsubscribe(msg.Unsubscribe.SubID)
			sess.ack(msg.ID, true, msg.Unsubscribe.SubID, 0, "")
		}
	case wire.CMsgPause:
		if msg.Pause != nil {
			if sub := sess.lookupSub(msg.Pause.SubID); sub != nil {
				sess.srv.store.Pause(sub.storeID)
			}
			sess.ack(msg.ID, true, msg.Pause.SubID, 0, "")
		}
	case wire.CMsgResume:
		if msg.Resume != nil {
			if sub := sess.lookupSub(msg.Resume.SubID); sub != nil {
				sess.srv.store.Resume(sub.storeID)
			}
			sess.ack(msg.ID, true, msg.Resume.SubID, 0, "")
		}
	case wire.CMsgFilter:
		sess.handleUpdateFilter(msg)
	case wire.CMsgAddSource:
		sess.handleAddSource(msg)
	case wire.CMsgRemoveSrc:
		if msg.RemoveSrc != nil {
			srcID := msg.RemoveSrc.SourceID
			// Capture kind/name before RemoveSource deletes the record so
			// the broadcast carries identifying info.
			ev := wire.SourceEvent{SourceID: srcID, State: "closed"}
			sess.srv.srcMu.RLock()
			if r, ok := sess.srv.srcs[srcID]; ok {
				ev.Kind = string(r.src.Kind())
				ev.Name = r.src.Name()
			}
			sess.srv.srcMu.RUnlock()
			if err := sess.srv.RemoveSource(srcID); err != nil {
				sess.errMsg(msg.ID, "remove_source", err.Error())
				return
			}
			sess.srv.broadcastSourceEvent(ev)
			sess.ack(msg.ID, true, 0, srcID, "")
		}
	case wire.CMsgStreamData:
		if msg.StreamData != nil {
			sess.handleStreamData(msg.StreamData)
		}
	case wire.CMsgHistory:
		sess.handleHistory(msg)
	case wire.CMsgPing:
		nonce := uint64(0)
		if msg.Ping != nil {
			nonce = msg.Ping.Nonce
		}
		_ = sess.conn.Write(&wire.ServerMsg{Type: wire.SMsgPong, Pong: &wire.Pong{Nonce: nonce}})
	case wire.CMsgActivateProfile:
		// Activation is server-global by design (sources live on the
		// server, not per-session) — last-tab wins. Clients echo the
		// switch on their own filter/columns logic; the server only
		// drives the sources overlay.
		var name string
		if msg.ActivateProfile != nil {
			name = msg.ActivateProfile.Name
		}
		if err := sess.srv.ActivateProfile(name); err != nil {
			sess.errMsg(msg.ID, "activate_profile", err.Error())
			return
		}
		sess.ack(msg.ID, true, 0, 0, "")
	default:
		sess.errMsg(msg.ID, "unknown_msg", "unknown type: "+msg.Type)
	}
}

func (sess *session) handleSubscribe(msg *wire.ClientMsg) {
	sub := msg.Subscribe
	if sub == nil {
		sess.errMsg(msg.ID, "bad_args", "subscribe missing payload")
		return
	}
	expr := sub.Filter
	// Profile resolution is per-client and server-side: we just compile the
	// expression as-is. Profile lookup happens in the client.
	node, err := filter.Parse(expr)
	if err != nil {
		sess.errMsg(msg.ID, "bad_filter", err.Error())
		return
	}
	fn := filter.Compile(node, sess.srv.store)
	storeSub := sess.srv.store.Subscribe(fn, sub.FromSeq)

	// If a sub with the same SubID is already registered (resubscribe after
	// reconnect with the same id), tear down the old one so its store
	// subscription doesn't leak.
	sess.subsMu.Lock()
	if old, ok := sess.subs[sub.SubID]; ok {
		sess.srv.store.Unsubscribe(old.storeID)
	}
	sess.subs[sub.SubID] = &sessionSub{
		subID:    sub.SubID,
		storeID:  storeSub.ID,
		node:     node,
		historyN: sub.HistoryN,
	}
	sess.subsMu.Unlock()

	// Drain backlog if requested using the bitmap plan, which prunes
	// candidate seqs via roaring intersections for indexed leaves before
	// running the residual closure.
	if sub.HistoryN > 0 {
		seqs := sess.queryBacklog(node, sub.HistoryN)
		_ = sess.sendEntries(sub.SubID, seqs, 0)
	}

	go sess.pumpSub(sub.SubID, storeSub.ID)
	sess.ack(msg.ID, true, sub.SubID, 0, "")
}

// queryBacklog returns the most recent up-to-limit matching seqs from the
// store ring (clamped to [tail, head)), in ascending order.
//
// Scans the entire ring rather than the last `limit` seqs — a sparse filter
// (e.g. a brushed time window in older history) might have all its matches
// outside the most-recent slice, which would otherwise return zero rows
// even though plenty of matches exist in the ring.
func (sess *session) queryBacklog(node filter.Node, limit int) []uint64 {
	if limit <= 0 {
		return nil
	}
	head := sess.srv.store.Head()
	tail := sess.srv.store.Tail()
	if head <= tail {
		return nil
	}
	plan := filter.CompilePlan(node, sess.srv.store)
	all := sess.srv.store.QueryRangeBitmap(plan.Candidates, plan.Residual, tail, head, 0)
	if len(all) <= limit {
		return all
	}
	return all[len(all)-limit:]
}

func (sess *session) handleUpdateFilter(msg *wire.ClientMsg) {
	if msg.Filter == nil {
		return
	}
	sub := sess.lookupSub(msg.Filter.SubID)
	if sub == nil {
		sess.errMsg(msg.ID, "no_sub", "")
		return
	}
	node, err := filter.Parse(msg.Filter.Filter)
	if err != nil {
		sess.errMsg(msg.ID, "bad_filter", err.Error())
		return
	}
	fn := filter.Compile(node, sess.srv.store)
	sess.srv.store.SetFilter(sub.storeID, fn)

	// Cache the new node so future history requests use it.
	sess.subsMu.Lock()
	sub.node = node
	sess.subsMu.Unlock()

	// Send a backfill so the client doesn't lose visible history when the
	// filter changes. Without this, the client clears its view and the
	// server would only stream live rows going forward.
	limit := sub.historyN
	if limit <= 0 {
		limit = 300
	}
	seqs := sess.queryBacklog(node, limit)
	_ = sess.sendEntries(msg.Filter.SubID, seqs, 0)

	sess.ack(msg.ID, true, msg.Filter.SubID, 0, "")
}

// handleHistory walks the ring backward from before_seq, returning up to
// limit matching seqs against the subscription's current filter. End=true
// means the range was exhausted (older entries unavailable in the buffer).
func (sess *session) handleHistory(msg *wire.ClientMsg) {
	h := msg.History
	if h == nil {
		return
	}
	sub := sess.lookupSub(h.SubID)
	if sub == nil {
		sess.errMsg(msg.ID, "no_sub", "")
		return
	}
	limit := h.Limit
	if limit <= 0 {
		limit = 200
	}

	tail := sess.srv.store.Tail()
	hi := min(h.BeforeSeq, sess.srv.store.Head())

	if hi <= tail {
		_ = sess.conn.Write(&wire.ServerMsg{Type: wire.SMsgBatch, Batch: &wire.LogBatch{
			SubID: h.SubID, IsHistory: true, End: true,
		}})
		sess.ack(msg.ID, true, h.SubID, 0, "")
		return
	}

	plan := filter.CompilePlan(sub.node, sess.srv.store)
	// Forward scan over the whole history range, keeping a sliding window of
	// the last `limit` matches via a ring buffer. Cheaper than collecting
	// every match into a slice when the filter is loose.
	all := sess.srv.store.QueryRangeBitmap(plan.Candidates, plan.Residual, tail, hi, 0)
	end := len(all) <= limit
	var seqs []uint64
	if len(all) <= limit {
		seqs = all
	} else {
		seqs = all[len(all)-limit:]
	}

	entries := make([]wire.Entry, 0, len(seqs))
	for _, seq := range seqs {
		row := sess.srv.store.Materialize(seq)
		if row == nil {
			continue
		}
		entries = append(entries, materializeToEntry(row))
	}
	_ = sess.conn.Write(&wire.ServerMsg{Type: wire.SMsgBatch, Batch: &wire.LogBatch{
		SubID:     h.SubID,
		Entries:   entries,
		IsHistory: true,
		End:       end,
	}})
	sess.ack(msg.ID, true, h.SubID, 0, "")
}

func (sess *session) handleAddSource(msg *wire.ClientMsg) {
	if msg.AddSource == nil {
		return
	}
	a := msg.AddSource
	switch a.Kind {
	case "file":
		path, _ := a.Args["path"].(string)
		if path == "" && a.Name != "" {
			path = a.Name
		}
		if path == "" {
			sess.errMsg(msg.ID, "bad_args", "file source requires path")
			return
		}
		id, err := sess.srv.AddFileSource(path)
		if err != nil {
			sess.errMsg(msg.ID, "add_failed", err.Error())
			return
		}
		sess.srv.broadcastSourceState(id, "open", "")
		sess.ack(msg.ID, true, 0, id, "")
	case "stdin":
		name := a.Name
		id, src, err := sess.srv.AddStdinSource(name)
		if err != nil {
			sess.errMsg(msg.ID, "add_failed", err.Error())
			return
		}
		sess.stdinSrcs[id] = src
		sess.srv.broadcastSourceState(id, "open", "")
		sess.ack(msg.ID, true, 0, id, "")
	case "docker":
		id, err := sess.srv.AddDockerSource(a.Name)
		if err != nil {
			sess.errMsg(msg.ID, "add_failed", err.Error())
			return
		}
		sess.srv.broadcastSourceState(id, "open", "")
		sess.ack(msg.ID, true, 0, id, "")
	default:
		sess.errMsg(msg.ID, "bad_kind", "unknown source kind: "+a.Kind)
	}
}

func (sess *session) handleStreamData(d *wire.StreamData) {
	src, ok := sess.stdinSrcs[d.SourceID]
	if !ok {
		return
	}
	src.Push(d.Chunk, d.EOF)
	if d.EOF {
		delete(sess.stdinSrcs, d.SourceID)
	}
}

func (sess *session) pumpSub(subID, storeID uint64) {
	out := sess.srv.store.Out(storeID)
	done := sess.srv.store.Done(storeID)
	if out == nil || done == nil {
		return
	}
	const batchSize = 100
	const flushInterval = 50 * time.Millisecond
	batch := make([]uint64, 0, batchSize)

	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	flush := func() {
		gap := sess.srv.store.TakeDropped(storeID)
		if len(batch) == 0 && gap == 0 {
			return
		}
		_ = sess.sendEntries(subID, batch, gap)
		batch = batch[:0]
	}

	for {
		select {
		case <-done:
			// Subscription cancelled by caller — abandon queued rows.
			flush()
			return
		case <-sess.srv.Done():
			// Server shutting down — best-effort drain so the client sees
			// the last few rows it would otherwise miss.
		drainShutdown:
			for {
				select {
				case ev := <-out:
					batch = append(batch, ev.Seq)
					if len(batch) >= batchSize {
						flush()
					}
				default:
					break drainShutdown
				}
			}
			flush()
			return
		case ev := <-out:
			batch = append(batch, ev.Seq)
			if len(batch) >= batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (sess *session) sendEntries(subID uint64, seqs []uint64, gap uint64) error {
	entries := make([]wire.Entry, 0, len(seqs))
	for _, seq := range seqs {
		row := sess.srv.store.Materialize(seq)
		if row == nil {
			continue
		}
		entries = append(entries, materializeToEntry(row))
	}
	return sess.conn.Write(&wire.ServerMsg{
		Type: wire.SMsgBatch,
		Batch: &wire.LogBatch{
			SubID:   subID,
			Entries: entries,
			GapN:    gap,
		},
	})
}

func materializeToEntry(row *store.MaterializedRow) wire.Entry {
	return wire.Entry{
		Seq:      row.Seq,
		Ts:       row.Ts,
		SourceID: row.SourceID,
		Level:    row.Level,
		Service:  row.Service,
		Msg:      row.Msg,
		Fields:   row.Fields,
		Ansi:     row.Ansi,
		Text:     row.Text,
	}
}

func (sess *session) lookupSub(subID uint64) *sessionSub {
	sess.subsMu.Lock()
	defer sess.subsMu.Unlock()
	return sess.subs[subID]
}

func (sess *session) unsubscribe(subID uint64) {
	sess.subsMu.Lock()
	sub, ok := sess.subs[subID]
	delete(sess.subs, subID)
	sess.subsMu.Unlock()
	if ok {
		sess.srv.store.Unsubscribe(sub.storeID)
	}
}

func (sess *session) ack(refID uint64, ok bool, subID, srcID uint64, detail string) {
	_ = sess.conn.Write(&wire.ServerMsg{
		Type: wire.SMsgAck,
		Ack:  &wire.Ack{OK: ok, RefID: refID, SubID: subID, SrcID: srcID, Detail: detail},
	})
}

func (sess *session) errMsg(refID uint64, code, detail string) {
	_ = sess.conn.Write(&wire.ServerMsg{
		Type: wire.SMsgErr,
		Err:  &wire.ErrMsg{RefID: refID, Code: code, Detail: detail},
	})
}


