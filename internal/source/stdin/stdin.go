// Package stdin implements a server-side adapter for the "stdin" source kind.
// A `loggi stdin` client opens a control connection and pushes StreamData
// frames; the server reassembles them into newline-delimited lines and emits
// them as RawLines.
package stdin

import (
	"bytes"
	"context"
	"sync"
	"sync/atomic"

	"github.com/iOliverNguyen/loggi/internal/source"
)

type Source struct {
	id     uint64
	name   string
	mu     sync.Mutex
	buf    bytes.Buffer
	notify chan struct{}
	closed atomic.Bool
}

func New(id uint64, name string) *Source {
	if name == "" {
		name = "stdin"
	}
	return &Source{id: id, name: name, notify: make(chan struct{}, 1)}
}

func (s *Source) ID() uint64        { return s.id }
func (s *Source) Kind() source.Kind { return source.KindStdin }
func (s *Source) Name() string      { return s.name }

// Push appends bytes from the client. When eof is true the source will close
// after the buffer drains.
func (s *Source) Push(chunk []byte, eof bool) {
	s.mu.Lock()
	if len(chunk) > 0 {
		s.buf.Write(chunk)
	}
	if eof {
		s.closed.Store(true)
	}
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

func (s *Source) Run(ctx context.Context, out chan<- source.RawLine) error {
	for {
		s.mu.Lock()
		// Flush all complete lines from buf.
		for {
			line, err := s.buf.ReadBytes('\n')
			if err != nil {
				if len(line) > 0 {
					// No newline yet; put it back.
					rest := append([]byte{}, s.buf.Bytes()...)
					s.buf.Reset()
					s.buf.Write(line)
					s.buf.Write(rest)
				}
				break
			}
			if line[len(line)-1] == '\n' {
				line = line[:len(line)-1]
			}
			if len(line) == 0 {
				continue
			}
			cp := make([]byte, len(line))
			copy(cp, line)
			s.mu.Unlock()
			select {
			case out <- source.RawLine{SourceID: s.id, Bytes: cp}:
			case <-ctx.Done():
				return ctx.Err()
			}
			s.mu.Lock()
		}
		closed := s.closed.Load()
		empty := s.buf.Len() == 0
		s.mu.Unlock()
		if closed && empty {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.notify:
		}
	}
}

func (s *Source) Close() error {
	s.closed.Store(true)
	select {
	case s.notify <- struct{}{}:
	default:
	}
	return nil
}
