package server

import (
	"sync"
	"time"

	"github.com/iOliverNguyen/loggi/internal/coldetect"
	"github.com/iOliverNguyen/loggi/internal/config"
	"github.com/iOliverNguyen/loggi/internal/source"
	"github.com/iOliverNguyen/loggi/internal/wire"
)

// columnDetector runs the per-source column sampler. One detector per
// sourceRec; nil once the source's column set is locked (either freshly
// detected or loaded from a persisted SourcePref). The wrapping mutex
// guards Observe/Recommend against the deadline-tick goroutine; the
// sampler itself isn't goroutine-safe.
type columnDetector struct {
	mu      sync.Mutex
	sampler *coldetect.Sampler
}

func (cd *columnDetector) observe(raw []byte) (done bool, finished bool, cols []string) {
	cd.mu.Lock()
	defer cd.mu.Unlock()
	if cd.sampler == nil {
		return true, false, nil
	}
	if cd.sampler.Observe(raw) {
		cols = cd.sampler.Recommend()
		cd.sampler = nil
		return true, true, cols
	}
	return false, false, nil
}

// closeIfDeadline finalizes the sampler if the wall-clock window has
// expired. Returns the recommendation when the detector closed on this
// call; (nil, false) when sampling is still active or already finished.
func (cd *columnDetector) closeIfDeadline() (cols []string, finished bool) {
	cd.mu.Lock()
	defer cd.mu.Unlock()
	if cd.sampler == nil || !cd.sampler.Deadline() {
		return nil, false
	}
	cols = cd.sampler.Recommend()
	cd.sampler = nil
	return cols, true
}

// lookupSourcePref reads the persisted column preferences for the given
// (kind, name) source key, or returns (nil, false) if none exists.
// Reads from disk on every call — invoked rarely (source attach,
// snapshot send), so a TOML round-trip is cheap enough.
func lookupSourcePref(kind source.Kind, name string) (*config.SourcePref, bool) {
	user, err := config.LoadUser()
	if err != nil {
		return nil, false
	}
	for i := range user.SourcePrefs {
		p := &user.SourcePrefs[i]
		if p.Kind == string(kind) && p.Name == name {
			return p, true
		}
	}
	return nil, false
}

// persistSourcePref writes a SourcePref keyed on (kind, name), replacing
// any existing entry with the same key. Best-effort: a write failure is
// logged but doesn't propagate — the column recommendation is already
// broadcast to live clients, so worst case is that detection re-runs on
// next restart for this source.
func (s *Server) persistSourcePref(kind, name string, cols []string) {
	user, err := config.LoadUser()
	if err != nil {
		s.logger.Printf("coldetect: load user config: %v", err)
		return
	}
	updated := false
	for i := range user.SourcePrefs {
		if user.SourcePrefs[i].Kind == kind && user.SourcePrefs[i].Name == name {
			user.SourcePrefs[i].Columns = cols
			user.SourcePrefs[i].Locked = true
			user.SourcePrefs[i].DetectedAt = time.Now().Unix()
			updated = true
			break
		}
	}
	if !updated {
		user.SourcePrefs = append(user.SourcePrefs, config.SourcePref{
			Kind:       kind,
			Name:       name,
			Columns:    cols,
			Locked:     true,
			DetectedAt: time.Now().Unix(),
		})
	}
	if err := config.SaveUser(user); err != nil {
		s.logger.Printf("coldetect: save user config: %v", err)
	}
}

// finalizeRecommendation persists the column set and broadcasts a source
// event so live clients install the columns immediately. Called from the
// ingest hot path when the sampler closes, and from the deadline checker.
func (s *Server) finalizeRecommendation(id uint64, cols []string) {
	if len(cols) == 0 {
		return
	}
	s.srcMu.Lock()
	rec, ok := s.srcs[id]
	var state string
	if ok {
		rec.pinnedColumns = cols
		state = rec.state
	}
	s.srcMu.Unlock()
	if !ok {
		return
	}
	kind := string(rec.src.Kind())
	name := rec.src.Name()
	s.persistSourcePref(kind, name, cols)

	ev := wire.SourceEvent{
		SourceID: id,
		Kind:     kind,
		Name:     name,
		State:    state,
		Mode:     rec.modeStr(),
		Columns:  cols,
	}
	s.broadcastSourceEvent(ev)
}
