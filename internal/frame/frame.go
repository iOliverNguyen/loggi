// Package frame implements 4-byte length-prefixed JSON framing for the unix
// socket transport. Each frame: [u32 BE length][JSON payload of that length].
package frame

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// MaxFrame guards against runaway allocations from a malformed peer.
const MaxFrame = 64 << 20 // 64 MiB

// Read reads one length-prefixed JSON frame from r and unmarshals it into v.
func Read(r io.Reader, v any) error {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 {
		return errors.New("frame: zero-length frame")
	}
	if n > MaxFrame {
		return fmt.Errorf("frame: oversize frame %d > %d", n, MaxFrame)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return err
	}
	return json.Unmarshal(buf, v)
}

// Write marshals v to JSON and writes one length-prefixed frame.
func Write(w io.Writer, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(payload) > MaxFrame {
		return fmt.Errorf("frame: oversize payload %d > %d", len(payload), MaxFrame)
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(payload)
	return err
}
