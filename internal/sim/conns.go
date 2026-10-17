package sim

import (
	"net"
	"sync"
)

// trackedListener remembers every connection it accepts so Close can end
// them along with the accept loop. Closing a plain listener leaves accepted
// connections serving, so a stopped (or darkened) device would keep
// answering clients that were already connected.
type trackedListener struct {
	net.Listener
	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	closed bool
}

func trackConns(l net.Listener) *trackedListener {
	return &trackedListener{Listener: l, conns: map[net.Conn]struct{}{}}
}

func (l *trackedListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		_ = c.Close()
		return nil, net.ErrClosed
	}
	tc := &trackedConn{Conn: c, owner: l}
	l.conns[tc] = struct{}{}
	return tc, nil
}

// Close stops accepting and closes every live accepted connection.
func (l *trackedListener) Close() error {
	l.mu.Lock()
	l.closed = true
	conns := l.conns
	l.conns = map[net.Conn]struct{}{}
	l.mu.Unlock()
	err := l.Listener.Close()
	for c := range conns {
		_ = c.(*trackedConn).Conn.Close()
	}
	return err
}

type trackedConn struct {
	net.Conn
	owner *trackedListener
	once  sync.Once
}

func (c *trackedConn) Close() error {
	c.once.Do(func() {
		c.owner.mu.Lock()
		delete(c.owner.conns, c)
		c.owner.mu.Unlock()
	})
	return c.Conn.Close()
}
