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

	pr, pw := io.Pipe()
	prErr, pwErr := io.Pipe()
	go func() {
		_, _ = stdcopy.StdCopy(pw, pwErr, rc)
		_ = pw.Close()
		_ = pwErr.Close()
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
	return nil
}

func (s *Source) scan(ctx context.Context, r io.Reader, out chan<- source.RawLine) {
	br := bufio.NewReaderSize(r, 1<<20)
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
