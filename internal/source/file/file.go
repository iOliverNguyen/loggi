// Package file implements a file-tailing source. Reads existing content from
// the start, then follows new writes. Handles truncation/rotation via stat
// polling (fsnotify is convenient but its semantics across platforms vary;
// stat polling is good enough here).
package file

import (
	"bufio"
	"context"
	"io"
	"os"
	"sync/atomic"
	"time"

	"github.com/iOliverNguyen/loggi/internal/source"
)

type Source struct {
	id     uint64
	path   string
	pollMS int // tail-polling cadence; 0 → defaultPollMS
	closed atomic.Bool
}

const defaultPollMS = 50

func New(id uint64, path string, pollMS int) *Source {
	return &Source{id: id, path: path, pollMS: pollMS}
}

func (s *Source) ID() uint64        { return s.id }
func (s *Source) Kind() source.Kind { return source.KindFile }
func (s *Source) Name() string      { return s.path }

func (s *Source) Run(ctx context.Context, out chan<- source.RawLine) error {
	f, err := os.Open(s.path)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	pos := int64(0)
	for {
		if s.closed.Load() {
			return nil
		}
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			if line[len(line)-1] == '\n' {
				line = line[:len(line)-1]
			}
			cp := make([]byte, len(line))
			copy(cp, line)
			select {
			case out <- source.RawLine{SourceID: s.id, Bytes: cp}:
			case <-ctx.Done():
				return ctx.Err()
			}
			pos += int64(len(line)) + 1
		}
		if err == nil {
			continue
		}
		if err != io.EOF {
			return err
		}
		// EOF: wait for new data, watch for truncation.
		if err := s.waitForGrowth(ctx, f, &pos); err != nil {
			return err
		}
		// Refresh reader state after potential rotation.
		r.Reset(f)
	}
}

func (s *Source) waitForGrowth(ctx context.Context, f *os.File, pos *int64) error {
	ms := s.pollMS
	if ms <= 0 {
		ms = defaultPollMS
	}
	t := time.NewTicker(time.Duration(ms) * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if s.closed.Load() {
				return nil
			}
			st, err := os.Stat(s.path)
			if err != nil {
				continue
			}
			if st.Size() < *pos {
				// Truncated; reopen.
				if _, err := f.Seek(0, io.SeekStart); err == nil {
					*pos = 0
				}
				return nil
			}
			if st.Size() > *pos {
				return nil
			}
		}
	}
}

func (s *Source) Close() error {
	s.closed.Store(true)
	return nil
}
