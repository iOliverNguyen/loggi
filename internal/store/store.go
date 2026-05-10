// Package store implements the in-memory log engine: a fixed-capacity ring of
// rows with columnar dictionary encoding for hot fields, a global string
// interner, and a content-addressed blob slab for large repetitive payloads.
package store

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RoaringBitmap/roaring/v2"
)

// Options configures a new Store.
type Options struct {
	Cap uint64 // ring capacity in entries; rounded up to power of two; default 524288
}

// Store holds all in-memory log state. Methods are safe for concurrent use.
type Store struct {
	mu sync.RWMutex

	cap  uint64
	mask uint64

	head uint64 // next seq id to assign (atomic-loaded under mu)
	tail uint64 // lowest live seq id

	// Hot column registry, keyed by JSON field name. Promotion-protected.
	hotMu sync.RWMutex
	hot   map[string]*Column

	// Tail KV slab: per ring slot, holds rare/cold fields not promoted to a
	// hot column. Each entry references the global interner.
	tailKV [][]kvPair

	// Per-row source id and JSON-mode flag (1=text/raw line)
	sourceID []uint64
	textMode []bool
	// Per-row blob ID for ANSI sidecar (text mode only).
	ansi []uint32

	intern *Interner
	blobs  *BlobSlab

	// Promotion tracking: per-key counters within a sliding window of recent
	// rows. Once a key fires the threshold it becomes a hot column.
	promoMu  sync.Mutex
	promoCnt map[string]uint32 // last-window counts
	rowsSeen uint64

	// SourceName resolves a source id to its display name. Set by the server
	// at startup; nil-safe via SourceName(). Lets the filter language refer
	// to sources by name (e.g. `source:my-api`) without baking the registry
	// into the store.
	sourceNameFn atomic.Pointer[func(uint64) string]

	// Subscribers
	subMu sync.RWMutex
	subs  map[uint64]*Subscriber
	subID atomic.Uint64

	// publishMu serializes publish fan-out so subscribers always observe
	// rows in seq order, even with concurrent Publish callers. Append's
	// per-row locking is via s.mu; publishMu only guards the broadcast.
	publishMu sync.Mutex

	closed atomic.Bool
}

type kvPair struct {
	keyID  uint32
	valID  uint32
	isBlob bool
}

const (
	defaultCap = uint64(524288)

	promoteThresh = 0.5

	// promoteCntMax bounds how many distinct cold keys we track for
	// promotion. Past this, new keys go straight to tail-KV without ever
	// being eligible for column promotion. Prevents unbounded map growth
	// on long-running servers ingesting diverse log shapes.
	promoteCntMax = 1024
)

// New constructs a Store.
func New(opts Options) *Store {
	cap := opts.Cap
	if cap == 0 {
		cap = defaultCap
	}
	cap = nextPow2(cap)

	s := &Store{
		cap:      cap,
		mask:     cap - 1,
		hot:      make(map[string]*Column),
		tailKV:   make([][]kvPair, cap),
		sourceID: make([]uint64, cap),
		textMode: make([]bool, cap),
		ansi:     make([]uint32, cap),
		intern:   NewInterner(),
		blobs:    NewBlobSlab(),
		promoCnt: make(map[string]uint32),
		subs:     make(map[uint64]*Subscriber),
	}
	// ts column always hot.
	s.hot["ts"] = newColumn("ts", ColF64, cap)
	// Pre-allocate the most common hot columns; they get filled lazily.
	// `source` is rendered as a UI column too, but synthesized from
	// SourceID at materialize time — not pre-allocated here.
	for _, k := range WellKnownHotFields {
		kind := ColDict
		if k == "msg" {
			kind = ColDict // msg also dict-encoded; high cardinality triggers Raw promotion
		}
		s.hot[k] = newColumn(k, kind, cap)
	}
	return s
}

// WellKnownHotFields is the set of pre-allocated hot column names. The
// HTTP `/api/columns` handler exposes this so the frontend doesn't need
// to hard-code a parallel list.
var WellKnownHotFields = []string{
	"level", "service", "env", "version", "msg", "caller", "callerFunc", "trace_id",
}

func nextPow2(n uint64) uint64 {
	if n == 0 {
		return 1
	}
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	n |= n >> 32
	return n + 1
}

// Cap returns the ring capacity.
func (s *Store) Cap() uint64 { return s.cap }

// Head returns the next-to-be-assigned sequence id.
func (s *Store) Head() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.head
}

// Tail returns the lowest still-live sequence id.
func (s *Store) Tail() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tail
}

// Intern returns the global string interner (used by filter compiler).
func (s *Store) Intern() *Interner { return s.intern }

// Blobs returns the blob slab.
func (s *Store) Blobs() *BlobSlab { return s.blobs }

// HotColumn returns the named hot column or nil.
func (s *Store) HotColumn(name string) *Column {
	s.hotMu.RLock()
	defer s.hotMu.RUnlock()
	return s.hot[name]
}

// HotColumnNames returns a snapshot of current hot-column names.
func (s *Store) HotColumnNames() []string {
	s.hotMu.RLock()
	defer s.hotMu.RUnlock()
	out := make([]string, 0, len(s.hot))
	for n := range s.hot {
		out = append(out, n)
	}
	return out
}

// AppendInput is the input to Append.
type AppendInput struct {
	SourceID uint64
	Ts       float64 // unix seconds; 0 = now
	JSON     []byte  // if non-nil, parsed as a JSON object
	Text     string  // plain text line (used when JSON is nil)
	AnsiBlob []byte  // original bytes including ANSI codes (text mode)
	Level    string  // hint for text-mode level extraction
}

// Append ingests one log row, returning its assigned sequence id.
//
// The whole row population runs under a single s.mu.Lock, so concurrent
// readers (Materialize, HotString, HotF64) either see no row at this seq
// (head not yet bumped from their RLock perspective) or a fully-populated
// one — never a partial state.
func (s *Store) Append(in AppendInput) uint64 {
	if s.closed.Load() {
		return 0
	}
	ts := in.Ts
	if ts == 0 {
		ts = float64(time.Now().UnixNano()) / 1e9
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	seq := s.head
	idx := seq & s.mask
	s.head++
	// Evict the row currently at idx (if any) before overwriting. The head
	// bump above ensures evictRowLocked sees the new head when computing
	// tail = head - cap. Readers (Materialize, HotString, HotF64) hold
	// s.mu.RLock and cannot proceed until we Unlock, so they never observe
	// a seq < head with an unpopulated slot.
	s.evictRowLocked(seq, idx)

	s.sourceID[idx] = in.SourceID
	s.hot["ts"].SetF64(idx, ts)

	switch {
	case in.JSON != nil:
		s.appendJSONLocked(seq, idx, in.JSON)
	default:
		s.appendTextLocked(seq, idx, in.Text, in.AnsiBlob, in.Level)
	}

	return seq
}

func (s *Store) appendJSONLocked(seq, idx uint64, raw []byte) {
	// Decode top-level keys into a map; nested values stay as RawMessage.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		// Fall back to text mode.
		s.appendTextLocked(seq, idx, string(raw), nil, "")
		return
	}
	s.textMode[idx] = false

	for k, rawVal := range obj {
		s.ingestFieldLocked(seq, idx, k, rawVal)
	}

	atomic.AddUint64(&s.rowsSeen, 1)
}

func (s *Store) appendTextLocked(seq, idx uint64, line string, ansiBytes []byte, levelHint string) {
	s.textMode[idx] = true
	if len(ansiBytes) > 0 {
		s.ansi[idx] = s.blobs.Put(ansiBytes)
	}
	s.hot["msg"].SetString(idx, seq, line, s.intern)
	if levelHint != "" {
		s.hot["level"].SetString(idx, seq, levelHint, s.intern)
	}
}

// ingestFieldLocked routes one JSON field to a hot column or tail KV slab.
// Caller must hold s.mu.
func (s *Store) ingestFieldLocked(seq, idx uint64, key string, raw json.RawMessage) {
	// Special-case ts as float.
	if key == "ts" {
		var f float64
		if err := json.Unmarshal(raw, &f); err == nil {
			s.hot["ts"].SetF64(idx, f)
			return
		}
	}

	// If hot, route directly.
	if c := s.hot[key]; c != nil {
		s.routeFieldToColumnLocked(seq, idx, c, raw)
		return
	}

	// Track for promotion.
	s.maybePromoteAndIngestLocked(seq, idx, key, raw)
}

func (s *Store) routeFieldToColumnLocked(seq, idx uint64, c *Column, raw json.RawMessage) {
	switch c.Kind {
	case ColF64:
		var f float64
		if err := json.Unmarshal(raw, &f); err == nil {
			c.SetF64(idx, f)
		}
	default:
		s.setColumnFromRawLocked(seq, idx, c, raw)
	}
}

func (s *Store) setColumnFromRawLocked(seq, idx uint64, c *Column, raw json.RawMessage) {
	// Strings → use as-is. Other JSON → keep raw bytes as the string value.
	if len(raw) > 0 && raw[0] == '"' {
		var str string
		if err := json.Unmarshal(raw, &str); err == nil {
			c.SetString(idx, seq, str, s.intern)
			return
		}
	}
	c.SetString(idx, seq, string(raw), s.intern)
}

func (s *Store) maybePromoteAndIngestLocked(seq, idx uint64, key string, raw json.RawMessage) {
	s.promoMu.Lock()
	if existing, ok := s.promoCnt[key]; ok {
		s.promoCnt[key] = existing + 1
	} else if len(s.promoCnt) < promoteCntMax {
		s.promoCnt[key] = 1
	}
	cnt := s.promoCnt[key]
	rows := atomic.LoadUint64(&s.rowsSeen)
	s.promoMu.Unlock()

	// Tally enough? Promote.
	if rows >= 64 && uint64(cnt)*100 >= rows*uint64(promoteThresh*100) && cnt >= 32 {
		s.hotMu.Lock()
		if _, exists := s.hot[key]; !exists {
			c := newColumn(key, ColDict, s.cap)
			s.hot[key] = c
		}
		c := s.hot[key]
		s.hotMu.Unlock()
		s.routeFieldToColumnLocked(seq, idx, c, raw)
		return
	}

	// Otherwise, write to the tail KV.
	s.appendTailKVLocked(idx, key, raw)
}

func (s *Store) appendTailKVLocked(idx uint64, key string, raw json.RawMessage) {
	keyID := s.intern.Intern(key)
	var valID uint32
	var isBlob bool
	switch {
	case len(raw) >= largeStringThreshold:
		valID = s.blobs.Put([]byte(raw))
		isBlob = true
	case len(raw) > 0 && raw[0] == '"':
		var str string
		if err := json.Unmarshal(raw, &str); err == nil {
			valID = s.intern.Intern(str)
		} else {
			valID = s.intern.Intern(string(raw))
		}
	default:
		valID = s.blobs.Put([]byte(raw))
		isBlob = true
	}
	s.tailKV[idx] = append(s.tailKV[idx], kvPair{keyID: keyID, valID: valID, isBlob: isBlob})
}

// evictRowLocked drops all column + tailKV references for the row at ring idx.
// Caller must hold s.mu.
func (s *Store) evictRowLocked(seq, idx uint64) {
	// Hot columns
	s.hotMu.RLock()
	cols := make([]*Column, 0, len(s.hot))
	for _, c := range s.hot {
		cols = append(cols, c)
	}
	s.hotMu.RUnlock()
	for _, c := range cols {
		c.EvictAt(idx, seq, s.intern)
	}
	// Tail KV
	for _, kv := range s.tailKV[idx] {
		s.intern.Release(kv.keyID)
		if kv.isBlob {
			s.blobs.Release(kv.valID)
		} else {
			s.intern.Release(kv.valID)
		}
	}
	s.tailKV[idx] = s.tailKV[idx][:0]
	// ANSI blob
	if s.ansi[idx] != 0 {
		s.blobs.Release(s.ansi[idx])
		s.ansi[idx] = 0
	}
	s.sourceID[idx] = 0
	s.textMode[idx] = false
	if s.head > s.cap {
		s.tail = s.head - s.cap
	}
}

// Close stops accepting new appends.
func (s *Store) Close() { s.closed.Store(true) }

// Subscriber represents an in-memory subscription.
//
// Concurrency: cursor is guarded by cursorMu (written by both publish and
// Resume). dead/paused are atomics. The out channel is never closed; pump
// goroutines must select on done to detect unsubscribe.
type Subscriber struct {
	ID uint64

	cursorMu sync.Mutex
	cursor   uint64

	filterMu sync.RWMutex
	filter   EvalFn

	paused atomic.Bool
	dead   atomic.Bool

	out  chan Event
	done chan struct{}

	// dropped accumulates rows skipped because out was full or because the
	// row was evicted before pause caught up. Pump callers consume via
	// TakeDropped; never reset directly to avoid losing concurrent updates.
	dropped atomic.Uint64

	store *Store
}

// EvalFn returns true if the row at seq matches the subscriber's filter.
// If filter is nil, the subscriber receives every row.
type EvalFn func(seq uint64) bool

// Event carries a delivered row's sequence id. Gap counts are reported
// separately via TakeDropped, not inline in events.
type Event struct {
	Seq uint64
}

const subChanCap = 1024

// Subscribe registers a new subscriber. fromSeq is the lowest seq the
// subscriber wants; 0 means "live tail from now (head)".
func (s *Store) Subscribe(filter EvalFn, fromSeq uint64) *Subscriber {
	id := s.subID.Add(1)
	cur := fromSeq
	if cur == 0 {
		cur = s.Head()
	}
	sub := &Subscriber{
		ID:     id,
		cursor: cur,
		filter: filter,
		out:    make(chan Event, subChanCap),
		done:   make(chan struct{}),
		store:  s,
	}
	s.subMu.Lock()
	s.subs[id] = sub
	s.subMu.Unlock()
	return sub
}

// Unsubscribe removes the subscriber. The out channel is NOT closed (would
// race with publishers); pump goroutines exit via the done channel.
func (s *Store) Unsubscribe(id uint64) {
	s.subMu.Lock()
	sub, ok := s.subs[id]
	delete(s.subs, id)
	s.subMu.Unlock()
	if !ok {
		return
	}
	if sub.dead.CompareAndSwap(false, true) {
		close(sub.done)
	}
}

// Pause stops live deliveries; new rows are not enqueued (they'll be
// re-walked on Resume).
func (s *Store) Pause(id uint64) {
	if sub := s.lookup(id); sub != nil {
		sub.paused.Store(true)
	}
}

// Resume walks cursor..head through the filter and delivers matching rows,
// then flips paused=false. Rows whose seq < tail (evicted while paused) are
// counted as gaps via dropped.
func (s *Store) Resume(id uint64) {
	sub := s.lookup(id)
	if sub == nil {
		return
	}

	sub.cursorMu.Lock()
	cur := sub.cursor
	sub.cursorMu.Unlock()

	head := s.Head()
	tail := s.Tail()

	if cur < tail {
		sub.dropped.Add(tail - cur)
		cur = tail
	}

	filter := sub.snapshotFilter()
	for c := cur; c < head; c++ {
		if filter == nil || filter(c) {
			s.deliver(sub, c)
		}
	}

	sub.cursorMu.Lock()
	if head > sub.cursor {
		sub.cursor = head
	}
	sub.cursorMu.Unlock()

	sub.paused.Store(false)
}

// SetFilter replaces a subscriber's filter compiled function.
func (s *Store) SetFilter(id uint64, f EvalFn) {
	if sub := s.lookup(id); sub != nil {
		sub.filterMu.Lock()
		sub.filter = f
		sub.filterMu.Unlock()
	}
}

// Out returns the receive channel for a subscriber.
func (s *Store) Out(id uint64) <-chan Event {
	if sub := s.lookup(id); sub != nil {
		return sub.out
	}
	return nil
}

// Done returns the cancellation channel for a subscriber. Pump goroutines
// should select on this to detect Unsubscribe.
func (s *Store) Done(id uint64) <-chan struct{} {
	if sub := s.lookup(id); sub != nil {
		return sub.done
	}
	return nil
}

// TakeDropped atomically reads-and-clears the subscriber's dropped counter.
// Returns 0 if the subscriber doesn't exist.
func (s *Store) TakeDropped(id uint64) uint64 {
	sub := s.lookup(id)
	if sub == nil {
		return 0
	}
	return sub.dropped.Swap(0)
}

func (s *Store) lookup(id uint64) *Subscriber {
	s.subMu.RLock()
	defer s.subMu.RUnlock()
	return s.subs[id]
}

func (sub *Subscriber) snapshotFilter() EvalFn {
	sub.filterMu.RLock()
	defer sub.filterMu.RUnlock()
	return sub.filter
}

// publish runs after each Append; called by the ingester after the row is
// fully populated.
func (s *Store) publish(seq uint64) {
	s.subMu.RLock()
	subs := make([]*Subscriber, 0, len(s.subs))
	for _, sub := range s.subs {
		subs = append(subs, sub)
	}
	s.subMu.RUnlock()
	for _, sub := range subs {
		if sub.dead.Load() {
			continue
		}
		if sub.paused.Load() {
			// Cursor stays put so Resume catches up. No row delivered.
			continue
		}
		filter := sub.snapshotFilter()
		if filter == nil || filter(seq) {
			s.deliver(sub, seq)
		}
		sub.cursorMu.Lock()
		if seq+1 > sub.cursor {
			sub.cursor = seq + 1
		}
		sub.cursorMu.Unlock()
	}
}

// deliver enqueues a row for sub. Bounded; if the channel is full, the row
// is counted as dropped and the consumer learns of it via TakeDropped.
//
// We never modify already-queued rows (no drop-oldest), preserving in-order
// delivery. Newer rows are dropped under backpressure.
func (s *Store) deliver(sub *Subscriber, seq uint64) {
	if sub.dead.Load() {
		return
	}
	select {
	case sub.out <- Event{Seq: seq}:
	case <-sub.done:
	default:
		sub.dropped.Add(1)
	}
}

// Publish appends and notifies all subscribers in the same critical section,
// guaranteeing that subscribers observe rows in seq order even when callers
// invoke Publish concurrently from multiple goroutines.
//
// Standalone Append() callers (test fixtures, batch loaders) bypass
// publishMu and lose this in-order guarantee — they should not be mixed
// with live subscribers.
func (s *Store) Publish(in AppendInput) uint64 {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	seq := s.Append(in)
	s.publish(seq)
	return seq
}

// Materialize builds the wire shape of a row at seq for client delivery.
// Returns nil if the row has been evicted. The whole materialization runs
// under s.mu.RLock to ensure snapshot consistency: an Append's eviction
// of the same ring slot is blocked until Materialize returns.
func (s *Store) Materialize(seq uint64) *MaterializedRow {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if seq < s.tail || seq >= s.head {
		return nil
	}
	idx := seq & s.mask

	row := &MaterializedRow{
		Seq:      seq,
		SourceID: s.sourceID[idx],
		Text:     s.textMode[idx],
	}
	if c := s.HotColumn("ts"); c != nil {
		if v, ok := c.GetF64(idx); ok {
			row.Ts = v
		}
	}
	if c := s.HotColumn("level"); c != nil {
		row.Level = c.GetString(idx, s.intern)
	}
	if c := s.HotColumn("service"); c != nil {
		row.Service = c.GetString(idx, s.intern)
	}
	if c := s.HotColumn("msg"); c != nil {
		row.Msg = c.GetString(idx, s.intern)
	}
	if row.Text && s.ansi[idx] != 0 {
		row.Ansi = string(s.blobs.Get(s.ansi[idx]))
	}

	// Build "fields" object from remaining hot columns + tail KV.
	fields := make(map[string]json.RawMessage)
	for _, name := range s.HotColumnNames() {
		switch name {
		case "ts", "level", "service", "msg":
			continue
		}
		c := s.HotColumn(name)
		if c == nil {
			continue
		}
		if c.Kind == ColF64 {
			if v, ok := c.GetF64(idx); ok {
				b, _ := json.Marshal(v)
				fields[name] = b
			}
			continue
		}
		v := c.GetString(idx, s.intern)
		if v != "" {
			b, _ := json.Marshal(v)
			fields[name] = b
		}
	}
	for _, kv := range s.tailKV[idx] {
		key := s.intern.Get(kv.keyID)
		if key == "" {
			continue
		}
		if kv.isBlob {
			fields[key] = json.RawMessage(s.blobs.Get(kv.valID))
		} else {
			b, _ := json.Marshal(s.intern.Get(kv.valID))
			fields[key] = b
		}
	}
	if len(fields) > 0 {
		raw, _ := json.Marshal(fields)
		row.Fields = raw
	}
	return row
}

// DebugSnapshot is a point-in-time snapshot of memory usage and ring
// state, intended for the /api/debug/store endpoint. Field names mirror
// the JSON we expose; reading is read-locked.
type DebugSnapshot struct {
	Cap     uint64              `json:"cap"`
	Head    uint64              `json:"head"`
	Tail    uint64              `json:"tail"`
	Rows    uint64              `json:"rows"`
	Intern  InternStats         `json:"intern"`
	Blobs   BlobStats           `json:"blobs"`
	Columns []ColumnStat        `json:"columns"`
}

type InternStats struct {
	Live       int `json:"live"`
	Free       int `json:"free"`
	ArenaBytes int `json:"arena_bytes"`
}

type BlobStats struct {
	Live  int `json:"live"`
	Free  int `json:"free"`
	Bytes int `json:"bytes"`
}

type ColumnStat struct {
	Name        string `json:"name"`
	Mode        string `json:"mode"` // "f64" | "dict" | "raw"
	Cardinality uint32 `json:"cardinality,omitempty"`
}

// Snapshot returns the current store stats. Cheap; intended for diagnostics.
//
// Each component (head/tail, interner, blob slab, columns) is read under
// its own lock, so the snapshot is internally consistent per-component
// but NOT transactional across them — a row appended between the head
// read and the column-cardinality read can show up as head++ without
// the corresponding bitmap delta. Acceptable for /api/debug/store; do
// not use this for anything correctness-critical.
func (s *Store) Snapshot() DebugSnapshot {
	s.mu.RLock()
	head, tail := s.head, s.tail
	s.mu.RUnlock()

	il, ifree, ibytes := s.intern.Stats()
	bl, bfree, bbytes := s.blobs.Stats()

	names := s.HotColumnNames()
	cols := make([]ColumnStat, 0, len(names))
	for _, n := range names {
		c := s.HotColumn(n)
		if c == nil {
			continue
		}
		cols = append(cols, ColumnStat{
			Name:        n,
			Mode:        columnModeName(c.Kind),
			Cardinality: c.Cardinality(),
		})
	}
	return DebugSnapshot{
		Cap:     s.cap,
		Head:    head,
		Tail:    tail,
		Rows:    head - tail,
		Intern:  InternStats{Live: il, Free: ifree, ArenaBytes: ibytes},
		Blobs:   BlobStats{Live: bl, Free: bfree, Bytes: bbytes},
		Columns: cols,
	}
}

func columnModeName(k ColumnKind) string {
	switch k {
	case ColF64:
		return "f64"
	case ColDict:
		return "dict"
	case ColRaw:
		return "raw"
	}
	return "?"
}

// SetSourceNameLookup registers (or replaces) the source-id → name resolver.
// Safe to call before or after Append.
func (s *Store) SetSourceNameLookup(fn func(uint64) string) {
	if fn == nil {
		s.sourceNameFn.Store(nil)
		return
	}
	s.sourceNameFn.Store(&fn)
}

// SourceName returns the registered display name for a source id, or "" if
// no resolver is set or the id is unknown.
func (s *Store) SourceName(id uint64) string {
	p := s.sourceNameFn.Load()
	if p == nil {
		return ""
	}
	return (*p)(id)
}

// SourceIDOfSeq returns the source id that ingested the row at seq. Returns
// 0 if the row has been evicted.
func (s *Store) SourceIDOfSeq(seq uint64) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if seq < s.tail || seq >= s.head {
		return 0
	}
	return s.sourceID[seq&s.mask]
}

// MaterializedRow is the materialized form for client delivery.
type MaterializedRow struct {
	Seq      uint64
	Ts       float64
	SourceID uint64
	Level    string
	Service  string
	Msg      string
	Fields   json.RawMessage
	Ansi     string
	Text     bool
}

// HotString returns the string value of a hot column for the given seq,
// or "" if missing/evicted/not-a-string column. Cheap: no full Materialize.
func (s *Store) HotString(seq uint64, name string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if seq < s.tail || seq >= s.head {
		return ""
	}
	c := s.HotColumn(name)
	if c == nil || c.Kind == ColF64 {
		return ""
	}
	return c.GetString(seq&s.mask, s.intern)
}

// HotF64 returns the float64 value of a hot column for seq.
func (s *Store) HotF64(seq uint64, name string) (float64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if seq < s.tail || seq >= s.head {
		return 0, false
	}
	c := s.HotColumn(name)
	if c == nil || c.Kind != ColF64 {
		return 0, false
	}
	return c.GetF64(seq & s.mask)
}

// QueryRangeBitmap is the bitmap-based fast path for historical queries.
// Iterates the candidate bitmap (or all live seqs when candidates==nil),
// applies residual, returns matching seqs in [lo, hi).
//
// Used by the filter package's Plan. For naive scan, see QueryRange.
func (s *Store) QueryRangeBitmap(candidates *roaring.Bitmap, residual EvalFn, lo, hi uint64, limit int) []uint64 {
	if hi == 0 {
		hi = s.Head()
	}
	tail := s.Tail()
	if lo < tail {
		lo = tail
	}
	if lo >= hi {
		return nil
	}
	out := make([]uint64, 0, 64)
	if candidates == nil {
		for seq := lo; seq < hi; seq++ {
			if residual != nil && !residual(seq) {
				continue
			}
			out = append(out, seq)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
		return out
	}
	it := candidates.Iterator()
	for it.HasNext() {
		seq := uint64(it.Next())
		if seq < lo {
			continue
		}
		if seq >= hi {
			break
		}
		if residual != nil && !residual(seq) {
			continue
		}
		out = append(out, seq)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// QueryRange returns up to limit row seqs in [lo, hi) matching filter.
// If filter is nil, all live rows are returned.
func (s *Store) QueryRange(filter EvalFn, lo, hi uint64, limit int) []uint64 {
	if hi == 0 {
		hi = s.Head()
	}
	if lo < s.Tail() {
		lo = s.Tail()
	}
	out := make([]uint64, 0, 64)
	for seq := lo; seq < hi; seq++ {
		if filter != nil && !filter(seq) {
			continue
		}
		out = append(out, seq)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// IndexAndOr is a helper used by the filter compiler — returns a snapshot
// bitmap of all live seqs. Cheap; runs once per query plan.
func (s *Store) AllLive() *roaring.Bitmap {
	bm := roaring.New()
	bm.AddRange(s.Tail(), s.Head())
	return bm
}
