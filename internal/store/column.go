package store

import (
	"sync"

	"github.com/RoaringBitmap/roaring/v2"
	"github.com/cespare/xxhash/v2"
)

// ColumnKind selects the encoding for a hot column.
type ColumnKind uint8

const (
	ColDict ColumnKind = iota // dictionary-encoded strings
	ColRaw                    // global-interner ids (high cardinality string)
	ColF64                    // float64 (e.g. ts)
)

// Column stores a per-row value for a specific JSON field across the ring.
// The slice `ids` is sized to ring capacity; index = seq & (cap-1).
type Column struct {
	Name string
	Kind ColumnKind

	mu sync.RWMutex

	// Dict mode:
	//   ids[ringIdx] = dictID; dict[dictID] = global-interner id; refs[dictID] = #rows
	// Raw mode:
	//   ids[ringIdx] = global-interner id (interner manages refs)
	// F64 mode:
	//   floats[ringIdx] = value
	ids    []uint32
	floats []float64
	mask   uint32 // 1 if row at ringIdx is set, 0 if not

	// presence bitmap: row seq -> set if column has a value for that row
	presence []uint64

	// dict mode state
	byHash map[uint64]uint32
	dict   []dictSlot
	free   []uint32

	// Per-dictID inverted index (Dict mode only): bitmap of seq32s.
	bitmaps map[uint32]*roaring.Bitmap

	cardinality uint32

	// Promotion threshold for flipping Dict -> Raw
	maxDict uint32
}

type dictSlot struct {
	internID uint32
	hash     uint64
	refs     uint32
	live     bool
}

func newColumn(name string, kind ColumnKind, cap uint64) *Column {
	c := &Column{
		Name:    name,
		Kind:    kind,
		maxDict: 16384,
		bitmaps: make(map[uint32]*roaring.Bitmap),
	}
	switch kind {
	case ColDict, ColRaw:
		c.ids = make([]uint32, cap)
	case ColF64:
		c.floats = make([]float64, cap)
	}
	c.presence = make([]uint64, (cap+63)/64)
	if kind == ColDict {
		c.byHash = make(map[uint64]uint32)
		c.dict = append(c.dict, dictSlot{}) // reserve id 0
	}
	return c
}

func (c *Column) cap() uint64 {
	if c.Kind == ColF64 {
		return uint64(len(c.floats))
	}
	return uint64(len(c.ids))
}

func (c *Column) presentAt(idx uint64) bool {
	return c.presence[idx>>6]&(1<<(idx&63)) != 0
}

func (c *Column) setPresent(idx uint64) {
	c.presence[idx>>6] |= 1 << (idx & 63)
}

func (c *Column) clearPresent(idx uint64) {
	c.presence[idx>>6] &^= 1 << (idx & 63)
}

// SetString stores s for the row at ring index `idx` (= seq & (cap-1)).
// `seq` is the absolute sequence id (used in bitmaps).
// `intern` is the global interner (used in Raw mode and as backing for Dict mode).
// Returns dictID (Dict) or interner id (Raw); 0 if empty.
func (c *Column) SetString(idx, seq uint64, s string, intern *Interner) uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.evictAtLocked(idx, intern)

	if s == "" {
		return 0
	}
	switch c.Kind {
	case ColDict:
		h := xxhash.Sum64String(s)
		var id uint32
		if existing, ok := c.byHash[h]; ok && c.dictEqual(existing, s, intern) {
			id = existing
			c.dict[id].refs++
		} else {
			internID := intern.Intern(s)
			if n := len(c.free); n > 0 {
				id = c.free[n-1]
				c.free = c.free[:n-1]
				c.dict[id] = dictSlot{internID: internID, hash: h, refs: 1, live: true}
			} else {
				id = uint32(len(c.dict))
				c.dict = append(c.dict, dictSlot{internID: internID, hash: h, refs: 1, live: true})
			}
			if _, taken := c.byHash[h]; !taken {
				c.byHash[h] = id
			}
			c.cardinality++
			if c.cardinality > c.maxDict {
				c.promoteToRawLocked(intern)
				// fall through to Raw set below
				return c.setRawLocked(idx, seq, s, intern)
			}
		}
		c.ids[idx] = id
		c.setPresent(idx)
		bm, ok := c.bitmaps[id]
		if !ok {
			bm = roaring.New()
			c.bitmaps[id] = bm
		}
		bm.Add(uint32(seq)) // truncated; ring fits in uint32 ranges practically
		return id
	case ColRaw:
		return c.setRawLocked(idx, seq, s, intern)
	}
	return 0
}

func (c *Column) setRawLocked(idx, seq uint64, s string, intern *Interner) uint32 {
	id := intern.Intern(s)
	c.ids[idx] = id
	c.setPresent(idx)
	return id
}

// SetF64 stores a float64 for the row at ring idx.
func (c *Column) SetF64(idx uint64, v float64) {
	c.mu.Lock()
	c.floats[idx] = v
	c.setPresent(idx)
	c.mu.Unlock()
}

// EvictAt drops the row at ring idx, releasing any references.
func (c *Column) EvictAt(idx, seq uint64, intern *Interner) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictAtLocked(idx, intern)
	_ = seq
}

func (c *Column) evictAtLocked(idx uint64, intern *Interner) {
	if !c.presentAt(idx) {
		return
	}
	switch c.Kind {
	case ColDict:
		id := c.ids[idx]
		if id != 0 && int(id) < len(c.dict) {
			s := &c.dict[id]
			if s.refs > 0 {
				s.refs--
				if s.refs == 0 {
					intern.Release(s.internID)
					if got, ok := c.byHash[s.hash]; ok && got == id {
						delete(c.byHash, s.hash)
					}
					if c.bitmaps != nil {
						delete(c.bitmaps, id) // wholesale removal
					}
					s.live = false
					c.cardinality--
					c.free = append(c.free, id)
				}
			}
		}
		c.ids[idx] = 0
	case ColRaw:
		id := c.ids[idx]
		intern.Release(id)
		c.ids[idx] = 0
	case ColF64:
		c.floats[idx] = 0
	}
	c.clearPresent(idx)
}

// GetString returns the string at ring idx (Dict or Raw); "" if not present.
func (c *Column) GetString(idx uint64, intern *Interner) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.presentAt(idx) {
		return ""
	}
	switch c.Kind {
	case ColDict:
		id := c.ids[idx]
		if int(id) >= len(c.dict) {
			return ""
		}
		return intern.Get(c.dict[id].internID)
	case ColRaw:
		return intern.Get(c.ids[idx])
	}
	return ""
}

// GetF64 returns the value at ring idx, ok.
func (c *Column) GetF64(idx uint64) (float64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.presentAt(idx) {
		return 0, false
	}
	return c.floats[idx], true
}

// BitmapForValue returns the bitmap of seqs whose value equals s, in Dict mode.
// Returns nil if not Dict or value unknown.
func (c *Column) BitmapForValue(s string) *roaring.Bitmap {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Kind != ColDict {
		return nil
	}
	h := xxhash.Sum64String(s)
	id, ok := c.byHash[h]
	if !ok {
		return nil
	}
	if bm, ok := c.bitmaps[id]; ok {
		return bm.Clone()
	}
	return nil
}

// Cardinality returns the number of distinct dict values currently live.
func (c *Column) Cardinality() uint32 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cardinality
}

// DistinctStrings returns up to limit distinct dict values (used by msg substring scan).
// Pairs each with a clone of its bitmap.
func (c *Column) DistinctStrings(limit int, intern *Interner) []DictEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Kind != ColDict {
		return nil
	}
	out := make([]DictEntry, 0, len(c.bitmaps))
	for id, bm := range c.bitmaps {
		if int(id) >= len(c.dict) || !c.dict[id].live {
			continue
		}
		s := intern.Get(c.dict[id].internID)
		out = append(out, DictEntry{Value: s, Bitmap: bm.Clone()})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// DictEntry is one value + the bitmap of seqs that have it.
type DictEntry struct {
	Value  string
	Bitmap *roaring.Bitmap
}

func (c *Column) dictEqual(id uint32, s string, intern *Interner) bool {
	if int(id) >= len(c.dict) || !c.dict[id].live {
		return false
	}
	return intern.Get(c.dict[id].internID) == s
}

// promoteToRawLocked converts this column from Dict mode to Raw mode.
// Existing per-row dict ids are translated to interner ids and the dict +
// bitmaps are dropped. The column keeps its `ids` slice but reinterprets it.
func (c *Column) promoteToRawLocked(intern *Interner) {
	for idx := uint64(0); idx < c.cap(); idx++ {
		if !c.presentAt(idx) {
			continue
		}
		dictID := c.ids[idx]
		if dictID == 0 || int(dictID) >= len(c.dict) {
			continue
		}
		// Each row holds 1 ref on the dict slot; translate to a refed
		// interner id. The dict slot held one ref on the interner already; we
		// release a ref per row from the dict and bump the interner.
		s := &c.dict[dictID]
		if s.live {
			// Don't double-intern; reuse the same interner id by retaining it.
			intern.Retain(s.internID)
		}
		c.ids[idx] = s.internID
		// Decrement dict slot refs
		if s.refs > 0 {
			s.refs--
		}
	}
	// Free the original interner refs held by dict slots (each unique slot
	// originally added 1 ref to the interner; rows now hold those refs).
	for id := range c.dict {
		s := &c.dict[id]
		if s.live {
			intern.Release(s.internID)
		}
	}
	c.byHash = nil
	c.dict = nil
	c.free = nil
	c.bitmaps = nil
	c.cardinality = 0
	c.Kind = ColRaw
}
