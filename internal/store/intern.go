package store

import (
	"sync"

	"github.com/cespare/xxhash/v2"
)

// Interner is a refcounted string interner backed by 64 KB arena chunks. Each
// interned string yields a uint32 ID; refcount tracks how many rows reference
// it. When refcount hits zero the slot is freed (LIFO free list); the arena
// bytes are not reclaimed (would require compaction; the slot is reused on
// subsequent intern() calls but new bytes append to the arena).
//
// Strings >= largeStringThreshold are NOT placed in the interner — caller
// should route them to the blob slab instead.
type Interner struct {
	mu     sync.RWMutex
	byHash map[uint64]uint32 // xxhash64(s) -> id
	slots  []internSlot
	free   []uint32 // LIFO of freed ids
	arena  arena
}

type internSlot struct {
	off    uint64 // offset into arena
	length uint32
	refs   uint32
	hash   uint64 // for byHash deletion
	live   bool
}

const largeStringThreshold = 256

func NewInterner() *Interner {
	in := &Interner{byHash: make(map[uint64]uint32)}
	// Reserve id 0 as "invalid".
	in.slots = append(in.slots, internSlot{})
	return in
}

// Intern interns s and returns its id with refcount incremented by 1.
// If s is empty, returns 0.
func (in *Interner) Intern(s string) uint32 {
	if s == "" {
		return 0
	}
	h := xxhash.Sum64String(s)
	in.mu.Lock()
	defer in.mu.Unlock()
	if id, ok := in.byHash[h]; ok {
		// Verify (xxhash collisions are astronomically rare but still possible).
		if in.equal(id, s) {
			in.slots[id].refs++
			return id
		}
		// Collision: fall through and create a new slot. The map slot stays on
		// the first one we saw; this second copy will live un-shared (no
		// dedup). Acceptable for tail-of-distribution behavior.
	}
	off, length := in.arena.appendStr(s)
	var id uint32
	if n := len(in.free); n > 0 {
		id = in.free[n-1]
		in.free = in.free[:n-1]
		in.slots[id] = internSlot{off: off, length: length, refs: 1, hash: h, live: true}
	} else {
		id = uint32(len(in.slots))
		in.slots = append(in.slots, internSlot{off: off, length: length, refs: 1, hash: h, live: true})
	}
	if _, taken := in.byHash[h]; !taken {
		in.byHash[h] = id
	}
	return id
}

// Get returns the string for id. Returns "" for id 0.
func (in *Interner) Get(id uint32) string {
	if id == 0 {
		return ""
	}
	in.mu.RLock()
	defer in.mu.RUnlock()
	if int(id) >= len(in.slots) || !in.slots[id].live {
		return ""
	}
	s := &in.slots[id]
	return in.arena.read(s.off, s.length)
}

// Retain increments the refcount on id (used when one row stores the same id
// multiple times — uncommon but possible).
func (in *Interner) Retain(id uint32) {
	if id == 0 {
		return
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	in.slots[id].refs++
}

// Release decrements id's refcount; if it reaches zero the slot is recycled.
func (in *Interner) Release(id uint32) {
	if id == 0 {
		return
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	if int(id) >= len(in.slots) {
		return
	}
	s := &in.slots[id]
	if s.refs == 0 {
		return
	}
	s.refs--
	if s.refs == 0 {
		// Remove from byHash only if it points at this id (collision case).
		if got, ok := in.byHash[s.hash]; ok && got == id {
			delete(in.byHash, s.hash)
		}
		s.live = false
		in.free = append(in.free, id)
	}
}

// Stats returns approximate usage stats for diagnostics.
func (in *Interner) Stats() (live, free, arenaBytes int) {
	in.mu.RLock()
	defer in.mu.RUnlock()
	for i := range in.slots {
		if in.slots[i].live {
			live++
		}
	}
	return live, len(in.free), in.arena.size()
}

func (in *Interner) equal(id uint32, s string) bool {
	if int(id) >= len(in.slots) || !in.slots[id].live {
		return false
	}
	sl := &in.slots[id]
	if int(sl.length) != len(s) {
		return false
	}
	return in.arena.read(sl.off, sl.length) == s
}

// arena is an append-only byte arena split into 64 KB chunks.
type arena struct {
	chunks [][]byte
}

const arenaChunk = 64 << 10

func (a *arena) appendStr(s string) (off uint64, length uint32) {
	length = uint32(len(s))
	// If string is larger than a chunk, give it its own chunk.
	if len(s) >= arenaChunk {
		a.chunks = append(a.chunks, []byte(s))
		off = uint64(len(a.chunks)-1) << 32
		return
	}
	if len(a.chunks) == 0 || len(a.chunks[len(a.chunks)-1])+len(s) > arenaChunk {
		a.chunks = append(a.chunks, make([]byte, 0, arenaChunk))
	}
	idx := len(a.chunks) - 1
	cur := a.chunks[idx]
	pos := len(cur)
	a.chunks[idx] = append(cur, s...)
	off = (uint64(idx) << 32) | uint64(pos)
	return
}

func (a *arena) read(off uint64, length uint32) string {
	idx := int(off >> 32)
	pos := int(off & 0xFFFFFFFF)
	chunk := a.chunks[idx]
	return string(chunk[pos : pos+int(length)])
}

func (a *arena) size() int {
	total := 0
	for _, c := range a.chunks {
		total += cap(c)
	}
	return total
}
