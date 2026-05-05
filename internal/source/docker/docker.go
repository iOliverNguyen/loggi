// Package docker implements a docker container log source using the Docker
// Engine SDK. Logs are streamed with Follow=true and demuxed via stdcopy.
package docker

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"sync/atomic"

	"github.com/docker/docker/api/types/container"
	dclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/iOliverNguyen/loggi/internal/source"
)

type Source struct {
	id        uint64
	container string
	since     string
	cli       *dclient.Client
	closed    atomic.Bool
}

func New(id uint64, containerName, since string) (*Source, error) {
	cli, err := dclient.NewClientWithOpts(dclient.FromEnv, dclient.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &Source{id: id, container: containerName, since: since, cli: cli}, nil
}

func (s *Source) ID() uint64        { return s.id }
func (s *Source) Kind() source.Kind { return source.KindDocker }
func (s *Source) Name() string      { return s.container }

func (s *Source) Run(ctx context.Context, out chan<- source.RawLine) error {
	// Inspect first: a TTY-allocated container ships logs as a raw byte
	// stream (no per-chunk header), so stdcopy.StdCopy would fail or
	// mis-decode. Inspect tells us which decoder to use; it also gives an
	// early, clear error for missing-container / permission failures
	// (instead of the obscure stream errors that Follow=true would emit).
	info, err := s.cli.ContainerInspect(ctx, s.container)
	if err != nil {
		return err
	}
	tty := info.Config != nil && info.Config.Tty

	rc, err := s.cli.ContainerLogs(ctx, s.container, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Tail:       "100",
		Since:      s.since,
		Timestamps: false,
	})
	if err != nil {
		return err
	}
	defer rc.Close()

	if tty {
		// Raw byte stream — feed the scanner directly. stderr is folded
		// into the same stream by the daemon when Tty=true.
		s.scan(ctx, rc, out)
		return nil
	}

	// Multiplexed stream: demux into stdout/stderr pipes and scan each.
	pr, pw := io.Pipe()
	prErr, pwErr := io.Pipe()
	stdcopyErrCh := make(chan error, 1)
	go func() {
		_, err := stdcopy.StdCopy(pw, pwErr, rc)
		_ = pw.Close()
		_ = pwErr.Close()
		stdcopyErrCh <- err
	}()

	doneCh := make(chan struct{})
	go func() {
		s.scan(ctx, pr, out)
		close(doneCh)
	}()
	doneErrCh := make(chan struct{})
	go func() {
		s.scan(ctx, prErr, out)
		close(doneErrCh)
	}()
	<-doneCh
	<-doneErrCh
	stdcopyErr := <-stdcopyErrCh
	// EOF / context cancellation are normal terminations; only surface
	// real protocol errors (e.g. malformed multiplex header).
	if stdcopyErr != nil && stdcopyErr != io.EOF && ctx.Err() == nil {
		return stdcopyErr
	}
	return nil
}

func (s *Source) scan(ctx context.Context, r io.Reader, out chan<- source.RawLine) {
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		if s.closed.Load() {
			return
		}
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			line = bytes.TrimRight(line, "\n")
			cp := make([]byte, len(line))
			copy(cp, line)
			select {
			case out <- source.RawLine{SourceID: s.id, Bytes: cp}:
			case <-ctx.Done():
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *Source) Close() error {
	s.closed.Store(true)
	return s.cli.Close()
}
