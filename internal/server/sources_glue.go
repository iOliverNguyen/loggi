package server

import (
	"github.com/iOliverNguyen/loggi/internal/source"
	dockersrc "github.com/iOliverNguyen/loggi/internal/source/docker"
	filesrc "github.com/iOliverNguyen/loggi/internal/source/file"
	stdinsrc "github.com/iOliverNguyen/loggi/internal/source/stdin"
)

// newFileSource is the constructor entry point used by Server.AddFileSource.
func newFileSource(id uint64, path string, pollMS int) source.Source {
	return filesrc.New(id, path, pollMS)
}

// stdinSource is exposed via the Server API so a session can push bytes.
type stdinSource = stdinsrc.Source

func newStdinSource(id uint64, name string) *stdinSource { return stdinsrc.New(id, name) }

func newDockerSource(id uint64, name string, tail int) (source.Source, error) {
	return dockersrc.New(id, name, tail)
}
