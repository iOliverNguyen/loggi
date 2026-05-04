package store

import (
	"sync"

	"github.com/cespare/xxhash/v2"
)

// BlobSlab is a content-addressed, refcounted store for large byte payloads
// (stack traces, embedded config dumps, ANSI-colored line bytes). Identical
// payloads share a single allocation.
type BlobSlab struct {
	mu     sync.RWMutex
	byHash map[uint64]uint32
	slots  []blobSlot
	free   []uint32
}

type blobSlot struct {
	data []byte
	hash uint64
	refs uint32
	live bool
}

func NewBlobSlab() *BlobSlab {
	b := &BlobSlab{byHash: make(map[uint64]uint32)}
	b.slots = append(b.slots, blobSlot{}) // reserve id 0
	return b
}

// Put stores data and returns its id with refcount += 1. Caller may free their
// copy of data; the slab takes ownership of a fresh copy.
func (b *BlobSlab) Put(data []byte) uint32 {
	if len(data) == 0 {
		return 0
	}
	h := xxhash.Sum64(data)
	b.mu.Lock()
	defer b.mu.Unlock()
	if id, ok := b.byHash[h]; ok && b.equal(id, data) {
		b.slots[id].refs++
		return id
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	var id uint32
	if n := len(b.free); n > 0 {
		id = b.free[n-1]
		b.free = b.free[:n-1]
		b.slots[id] = blobSlot{data: cp, hash: h, refs: 1, live: true}
	} else {
		id = uint32(len(b.slots))
		b.slots = append(b.slots, blobSlot{data: cp, hash: h, refs: 1, live: true})
	}
	if _, taken := b.byHash[h]; !taken {
		b.byHash[h] = id
	}
	return id
}

// Get returns the bytes for id (or nil for id 0 / freed).
func (b *BlobSlab) Get(id uint32) []byte {
	if id == 0 {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if int(id) >= len(b.slots) || !b.slots[id].live {
		return nil
	}
	return b.slots[id].data
}

// Retain bumps refcount.
func (b *BlobSlab) Retain(id uint32) {
	if id == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.slots[id].refs++
}

// Release decrements; frees on zero.
func (b *BlobSlab) Release(id uint32) {
	if id == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if int(id) >= len(b.slots) {
		return
	}
	s := &b.slots[id]
	if s.refs == 0 {
		return
	}
	s.refs--
	if s.refs == 0 {
		if got, ok := b.byHash[s.hash]; ok && got == id {
			delete(b.byHash, s.hash)
		}
		s.data = nil
		s.live = false
		b.free = append(b.free, id)
	}
}

func (b *BlobSlab) equal(id uint32, data []byte) bool {
	if int(id) >= len(b.slots) || !b.slots[id].live {
		return false
	}
	d := b.slots[id].data
	if len(d) != len(data) {
		return false
	}
	for i := range d {
		if d[i] != data[i] {
			return false
		}
	}
	return true
}

// Stats reports approximate usage.
func (b *BlobSlab) Stats() (live, free, bytes int) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for i := range b.slots {
		if b.slots[i].live {
			live++
			bytes += len(b.slots[i].data)
		}
	}
	return live, len(b.free), bytes
}
