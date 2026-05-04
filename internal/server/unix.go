package server

import (
	"net"
	"sync"

	"github.com/iOliverNguyen/loggi/internal/frame"
	"github.com/iOliverNguyen/loggi/internal/wire"
)

// unixConn adapts a net.Conn to the Conn interface using length-prefixed JSON.
type unixConn struct {
	c    net.Conn
	wmu  sync.Mutex
}

func newUnixConn(c net.Conn) *unixConn { return &unixConn{c: c} }

func (u *unixConn) Read(v *wire.ClientMsg) error { return frame.Read(u.c, v) }

func (u *unixConn) Write(v *wire.ServerMsg) error {
	u.wmu.Lock()
	defer u.wmu.Unlock()
	return frame.Write(u.c, v)
}

func (u *unixConn) Close() error { return u.c.Close() }

func (s *Server) serveUnix(l net.Listener) {
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go s.runSession(s.ctx, newUnixConn(c))
	}
}
