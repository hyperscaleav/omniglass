package collection

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// The held line connection (#816): the line protocol's stateful form. One
// TCP connection stays open; a single reader goroutine frames inbound lines,
// hands the next one to the exchange waiting for it, and treats a line nobody
// asked for (a banner today, a device push once listeners arm, #817) as
// unsolicited. The reader is also the liveness signal: a peer that closes,
// or a read that fails, closes Done at once, without waiting for a poll.

// NewLineSessionDialer returns the real held line-connection dialer.
func NewLineSessionDialer() SessionDialer { return lineSessionDialer{} }

type lineSessionDialer struct{}

func (lineSessionDialer) Dial(ctx context.Context, target string, timeout time.Duration) (HeldConn, error) {
	if timeout <= 0 {
		timeout = defaultSessionTimeout
	}
	d := net.Dialer{Timeout: timeout, KeepAlive: SessionKeepAlive}
	c, err := d.DialContext(ctx, "tcp", target)
	if err != nil {
		return nil, fmt.Errorf("collection: line session dial %s: %w", target, err)
	}
	lc := &lineConn{c: c, target: target, done: make(chan struct{})}
	go lc.read()
	return lc, nil
}

// lineConn is one held line connection. xmu serializes exchanges (one
// request in flight, so the next line is its answer); mu guards the waiter
// the reader hands that line to.
type lineConn struct {
	c      net.Conn
	target string
	xmu    sync.Mutex

	mu     sync.Mutex
	waiter chan string
	err    error

	done      chan struct{}
	closeOnce sync.Once
}

// read frames inbound lines until the connection dies. A line longer than
// maxLineAnswer with no terminator kills the connection: the node is the
// whole collection agent for its placement and cannot buffer without bound.
func (c *lineConn) read() {
	sc := bufio.NewScanner(c.c)
	sc.Buffer(make([]byte, 0, 4096), maxLineAnswer)
	for sc.Scan() {
		line := trimCR(sc.Text())
		c.mu.Lock()
		w := c.waiter
		c.waiter = nil
		c.mu.Unlock()
		if w != nil {
			w <- line // buffered: the reader never blocks on a gone waiter
		}
		// An unclaimed line is unsolicited: dropped until listeners claim
		// pushes over the held session (#817).
	}
	err := sc.Err()
	switch {
	case errors.Is(err, bufio.ErrTooLong):
		err = fmt.Errorf("line exceeded %d bytes with no terminator", maxLineAnswer)
	case err == nil:
		err = io.EOF // the peer closed the connection
	}
	c.fail(err)
}

func trimCR(s string) string {
	if n := len(s); n > 0 && s[n-1] == '\r' {
		return s[:n-1]
	}
	return s
}

// fail records the first cause and closes the connection and Done.
func (c *lineConn) fail(err error) {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.err = err
		c.mu.Unlock()
		_ = c.c.Close()
		close(c.done)
	})
}

func (c *lineConn) Done() <-chan struct{} { return c.done }

func (c *lineConn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *lineConn) Close() error {
	c.fail(net.ErrClosed)
	return nil
}

// Exchange sends one line and returns the next line the device sends. A
// timeout fails this exchange and keeps the connection: a late answer then
// arrives with no waiter and is dropped as unsolicited. A write failure
// kills the connection, so the session recovers it.
func (c *lineConn) Exchange(ctx context.Context, line string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = defaultSessionTimeout
	}
	c.xmu.Lock()
	defer c.xmu.Unlock()
	select {
	case <-c.done:
		return "", fmt.Errorf("collection: line session %s: %w", c.target, c.Err())
	default:
	}
	w := make(chan string, 1)
	c.mu.Lock()
	c.waiter = w
	c.mu.Unlock()
	release := func() {
		c.mu.Lock()
		if c.waiter == w {
			c.waiter = nil
		}
		c.mu.Unlock()
	}
	_ = c.c.SetWriteDeadline(time.Now().Add(timeout))
	if _, err := fmt.Fprintf(c.c, "%s\r\n", line); err != nil {
		release()
		c.fail(err)
		return "", fmt.Errorf("collection: line session send %s: %w", c.target, err)
	}
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case answer := <-w:
		return answer, nil
	case <-c.done:
		release()
		return "", fmt.Errorf("collection: line session %s: %w", c.target, c.Err())
	case <-ctx.Done():
		release()
		return "", fmt.Errorf("collection: line session %s: %w", c.target, ctx.Err())
	case <-t.C:
		release()
		return "", fmt.Errorf("collection: line session %s: no answer within %s", c.target, timeout)
	}
}
