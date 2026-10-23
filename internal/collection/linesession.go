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
// TCP connection stays open and a single reader goroutine owns it. That
// goroutine is the liveness signal (a peer that closes, or a read that fails,
// closes Done at once, without waiting for a poll), and it is also the only
// place an ask is written, so the boundary between "before the ask" and
// "after the ask" is drawn on one goroutine: on an ask it discards every byte
// already received (a banner, a stray line; device pushes are claimed by
// listeners in #817), writes the line, and hands the next line it reads to
// that ask. An ask whose answer does not arrive in time recycles the
// connection, since a late answer would otherwise answer the next ask.

// ErrExchangeTimeout is the cause a held connection dies of when an
// exchange's answer did not arrive in time.
var ErrExchangeTimeout = errors.New("exchange timed out; connection recycled")

// drainWindow is how long the reader waits for bytes already on their way
// when it draws an ask's boundary. Bytes still in flight from before the ask
// (a banner sent on connect) land in this window and are discarded.
const drainWindow = 2 * time.Millisecond

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
	lc := &lineConn{
		c: c, target: target, br: bufio.NewReaderSize(c, maxLineAnswer),
		asks: make(chan lineAsk, 1), done: make(chan struct{}),
	}
	go lc.read()
	return lc, nil
}

// lineAsk is one exchange handed to the reader: the line to write and where
// its answer goes.
type lineAsk struct {
	line    string
	timeout time.Duration
	answer  chan lineAnswer
}

type lineAnswer struct {
	line string
	err  error
}

// lineConn is one held line connection. xmu serializes exchanges, so at most
// one ask is pending; the reader goroutine owns every read and every write.
type lineConn struct {
	c      net.Conn
	target string
	br     *bufio.Reader
	xmu    sync.Mutex
	asks   chan lineAsk

	mu  sync.Mutex
	err error

	done      chan struct{}
	closeOnce sync.Once
}

// read is the reader goroutine. armed is the ask awaiting its answer; a line
// read with nothing armed is unsolicited and dropped.
func (c *lineConn) read() {
	var armed chan lineAnswer
	for {
		raw, err := c.br.ReadSlice('\n')
		if err != nil {
			if isTimeout(err) {
				// An exchange interrupted the read to hand over its ask (or a
				// stale interrupt from one already served): clear the deadline
				// before looking, so a later interrupt is never lost.
				_ = c.c.SetReadDeadline(time.Time{})
				select {
				case ask := <-c.asks:
					armed = c.arm(ask)
				default:
				}
				continue
			}
			if errors.Is(err, bufio.ErrBufferFull) {
				err = fmt.Errorf("line exceeded %d bytes with no terminator", maxLineAnswer)
			}
			c.fail(err)
			return
		}
		if armed == nil {
			continue // unsolicited: dropped until listeners claim pushes (#817)
		}
		armed <- lineAnswer{line: trimLine(string(raw))} // buffered: never blocks
		armed = nil
	}
}

// arm draws an ask's boundary and writes it: everything already received is
// discarded, then the line goes out, and the next line read answers it. A
// write failure answers the ask with the error and kills the connection.
func (c *lineConn) arm(ask lineAsk) chan lineAnswer {
	_, _ = c.br.Discard(c.br.Buffered())
	scratch := make([]byte, 4096)
	_ = c.c.SetReadDeadline(time.Now().Add(drainWindow))
	for {
		if _, err := c.c.Read(scratch); err != nil {
			if isTimeout(err) {
				break
			}
			ask.answer <- lineAnswer{err: err}
			c.fail(err)
			return nil
		}
	}
	_ = c.c.SetReadDeadline(time.Time{})
	_ = c.c.SetWriteDeadline(time.Now().Add(ask.timeout))
	if _, err := fmt.Fprintf(c.c, "%s\r\n", ask.line); err != nil {
		ask.answer <- lineAnswer{err: fmt.Errorf("collection: line session send %s: %w", c.target, err)}
		c.fail(err)
		return nil
	}
	return ask.answer
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func trimLine(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

// fail records the first cause and closes the connection and Done.
func (c *lineConn) fail(err error) {
	c.closeOnce.Do(func() {
		if err == nil {
			err = io.EOF
		}
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

// Exchange hands one ask to the reader and waits for its answer. An answer
// that does not arrive in time kills the connection with ErrExchangeTimeout
// (an ask abandoned by its context is recycled too): its reply may still be
// on the way, and on a held connection it would answer the next ask.
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
	ask := lineAsk{line: line, timeout: timeout, answer: make(chan lineAnswer, 1)}
	c.asks <- ask // capacity 1, and xmu admits one ask at a time
	// Interrupt the reader's blocked read so it takes the ask now.
	_ = c.c.SetReadDeadline(time.Now())
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case a := <-ask.answer:
		if a.err != nil {
			return "", a.err
		}
		return a.line, nil
	case <-c.done:
		return "", fmt.Errorf("collection: line session %s: %w", c.target, c.Err())
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			c.fail(ErrExchangeTimeout) // the caller's deadline is the answer's
			return "", fmt.Errorf("collection: line session %s: no answer in time: %w", c.target, ErrExchangeTimeout)
		}
		c.fail(fmt.Errorf("exchange abandoned (%v); connection recycled", ctx.Err()))
		return "", fmt.Errorf("collection: line session %s: %w", c.target, ctx.Err())
	case <-t.C:
		c.fail(ErrExchangeTimeout)
		return "", fmt.Errorf("collection: line session %s: no answer within %s: %w", c.target, timeout, ErrExchangeTimeout)
	}
}
