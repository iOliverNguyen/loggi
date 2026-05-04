// Package source defines the interface for ingest sources (file, stdin, docker).
package source

import (
	"context"
	"sync/atomic"
)

// Kind identifies a source type for diagnostics and the wire protocol.
type Kind string

const (
	KindFile   Kind = "file"
	KindStdin  Kind = "stdin"
	KindDocker Kind = "docker"
)

// RawLine is one undecoded log line emitted by a Source. The ingester decides
// JSON vs text downstream.
type RawLine struct {
	SourceID uint64
	Bytes    []byte // line bytes; may include ANSI; no trailing newline
}

// Source is something that produces log lines until Close.
type Source interface {
	ID() uint64
	Kind() Kind
	Name() string
	Run(ctx context.Context, out chan<- RawLine) error
	Close() error
}

// IDGen returns monotonically increasing source IDs.
type IDGen struct{ n atomic.Uint64 }

func (g *IDGen) Next() uint64 { return g.n.Add(1) }
