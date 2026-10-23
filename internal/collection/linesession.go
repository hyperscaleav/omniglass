package collection

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The held line connection (#816): the line protocol's stateful form. One
// TCP connection stays open and a single reader goroutine owns it. That
// goroutine is the liveness signal (a peer that closes, or a read that fails,
// closes Done at once, without waiting for a poll), and it is also the only
// place an ask is written, so the boundary between "before the ask" and
// "after the ask" is drawn on one goroutine: on an ask it routes every line
// already received as received before the ask (a listener's push to the
// listener, a banner or stray line as unclaimed; never the answer), writes
// the line, and hands the ask its answer by the precedence complete
// documents (#817). An ask whose answer does not arrive in time recycles the
// connection, since a late answer would otherwise answer the next ask.

// ErrExchangeTimeout is the cause a held connection dies of when an
// exchange that declared no reply shape got no answer in time: its late
// reply would answer the next ask, so the connection is recycled.
var ErrExchangeTimeout = errors.New("exchange timed out; connection recycled")

// ErrNoAnswer fails an exchange that declared a reply shape and got no
// answer in time. Only that exchange fails: shape routing means a late
// answer can never be taken for the next ask's, so the connection is kept
// and the session is not charged.
var ErrNoAnswer = errors.New("no answer in time")

// RefusedError is an exchange answered by a line matching the request's
// declared error shape: the device refused, and Line is what it said.
type RefusedError struct {
	Request string
	Line    string
}

func (e *RefusedError) Error() string {
	return fmt.Sprintf("device refused %q: %s", e.Request, e.Line)
}

// drainWindow is how long the reader waits for bytes already on their way
// when it draws an ask's boundary. Bytes still in flight from before the ask
// (a banner sent on connect) land in this window and are discarded.
const drainWindow = 2 * time.Millisecond

// NewLineSessionDialer returns the real held line-connection dialer. It is
// staged (#1049): a declared login runs over the raw connection as the
// session's auth step before the reader starts routing lines.
func NewLineSessionDialer() StagedDialer { return lineSessionDialer{} }

// lineSessionDialer dials the line protocol over tcp, or over telnet (the
// same connection behind the telnet filter, #1033).
type lineSessionDialer struct{ telnet bool }

func (d lineSessionDialer) Dial(ctx context.Context, target string, timeout time.Duration, in Inbound) (HeldConn, error) {
	pc, err := d.Connect(ctx, target, timeout)
	if err != nil {
		return nil, err
	}
	return pc.Open(in, ChannelOptions{})
}

func (ld lineSessionDialer) Connect(ctx context.Context, target string, timeout time.Duration) (PendingConn, error) {
	if timeout <= 0 {
		timeout = defaultSessionTimeout
	}
	d := net.Dialer{Timeout: timeout, KeepAlive: SessionKeepAlive}
	c, err := d.DialContext(ctx, "tcp", target)
	if err != nil {
		return nil, fmt.Errorf("collection: line session dial %s: %w", target, err)
	}
	if ld.telnet {
		c = NewTelnetConn(c)
	}
	return &pendingLine{c: c, target: target}, nil
}

// pendingLine is a line connection made and not yet routing: the auth step
// reads and writes it raw, and Open hands it to the reader.
type pendingLine struct {
	c      net.Conn
	target string
}

// Authenticate runs the declared login dialog over the raw connection.
func (p *pendingLine) Authenticate(ctx context.Context, auth *SessionAuth, terminal Classifier, timeout time.Duration) error {
	if auth == nil || auth.Login == nil {
		return nil
	}
	if err := RunLogin(ctx, p.c, auth.Login, terminal, timeout); err != nil {
		if _, ok := AsTerminal(err); ok {
			return err
		}
		return fmt.Errorf("collection: line session %s: %w", p.target, err)
	}
	return nil
}

// Open starts the reader: from here the connection routes every line.
func (p *pendingLine) Open(in Inbound, _ ChannelOptions) (HeldConn, error) {
	return openLineConn(p.c, p.target, in), nil
}

func (p *pendingLine) Close() error { return p.c.Close() }

// openLineConn wraps a connection as a held line connection and starts its
// reader goroutine.
func openLineConn(c net.Conn, target string, in Inbound) *lineConn {
	lc := &lineConn{
		c: c, target: target, br: bufio.NewReaderSize(c, maxLineAnswer), in: in,
		asks: make(chan lineAsk, 1), done: make(chan struct{}),
	}
	go lc.read()
	return lc
}

// lineAsk is one exchange handed to the reader: the line to write, the
// declared reply shape (nil: the first line no listener claims), and where
// its answer goes.
type lineAsk struct {
	line    string
	reply   *driver.Reply
	timeout time.Duration
	answer  chan lineAnswer
	// gone is closed when the exchange gave up without recycling the
	// connection (a shaped timeout or abandonment): the reader drops it.
	gone chan struct{}
}

func (a *lineAsk) abandoned() bool {
	select {
	case <-a.gone:
		return true
	default:
		return false
	}
}

type lineAnswer struct {
	line    string
	refused bool
	err     error
}

// lineConn is one held line connection. xmu serializes exchanges, so at most
// one ask is pending; the reader goroutine owns every read and every write.
type lineConn struct {
	c      net.Conn
	target string
	br     *bufio.Reader
	in     Inbound
	xmu    sync.Mutex
	asks   chan lineAsk

	mu  sync.Mutex
	err error

	done      chan struct{}
	closeOnce sync.Once
}

// reader is the reader goroutine's state: the ask awaiting its answer, and
// the bytes of a line not yet complete (carry), with whether that line began
// before the armed ask was written (a line that began before the ask cannot
// be its answer).
type reader struct {
	c        *lineConn
	armed    *lineAsk
	carry    []byte
	carryPre bool
}

// read is the reader goroutine.
func (c *lineConn) read() {
	r := &reader{c: c}
	for {
		raw, err := c.br.ReadSlice('\n')
		if err != nil {
			if !isInterrupt(err) {
				r.die(err)
				return
			}
			_ = c.c.SetReadDeadline(time.Time{})
			if !r.keep(raw) {
				return
			}
			if r.armed != nil && r.armed.abandoned() {
				r.armed = nil // the exchange gave up; the next ask may be waiting
			}
			if r.armed != nil {
				// A late interrupt landed mid-answer: no ask can be pending
				// while one is armed, so the partial bytes are kept and the
				// read goes on.
				continue
			}
			// An exchange interrupted the read to hand over its ask (or a
			// stale interrupt from one already served): the deadline is
			// cleared before looking, so a later interrupt is never lost.
			select {
			case ask := <-c.asks:
				if !r.arm(ask) {
					return
				}
			default:
			}
			continue
		}
		if !r.keep(raw) {
			return
		}
		r.complete()
	}
}

// keep appends bytes to the carry, bounded like any line.
func (r *reader) keep(raw []byte) bool {
	r.carry = append(r.carry, raw...)
	if len(r.carry) > maxLineAnswer {
		r.c.fail(fmt.Errorf("line exceeded %d bytes with no terminator", maxLineAnswer))
		return false
	}
	return true
}

// complete routes the carried line, now terminated. The precedence: a line a
// listener claims is the listener's, and an exchange is never handed it
// unless the exchange declared a reply shape the line matches (then both
// have it); an armed exchange otherwise takes the first line that matches
// its declared answer shape, or, with none declared, the first line no
// listener claims; an unclaimed line matching its declared error shape
// answers it as a refusal; a line that began before the ask was written is
// never its answer; whatever is left is unclaimed.
func (r *reader) complete() {
	line, pre := trimLine(string(r.carry)), r.carryPre
	r.carry, r.carryPre = r.carry[:0], false
	claimed := r.c.in != nil && r.c.in.Claim(line)
	if r.armed != nil && r.armed.abandoned() {
		r.armed = nil
	}
	if a := r.armed; a != nil && !pre {
		switch {
		case (a.reply != nil && a.reply.Accepts(line)) || (a.reply == nil && !claimed):
			a.answer <- lineAnswer{line: line} // buffered: never blocks
			r.armed = nil
			return
		case !claimed && a.reply.Fails(line):
			a.answer <- lineAnswer{line: line, refused: true}
			r.armed = nil
			return
		}
	}
	if !claimed && r.c.in != nil {
		r.c.in.Unclaimed(line)
	}
}

// die records why the connection ended.
func (r *reader) die(err error) {
	if errors.Is(err, bufio.ErrBufferFull) {
		err = fmt.Errorf("line exceeded %d bytes with no terminator", maxLineAnswer)
	}
	r.c.fail(err)
}

// arm draws an ask's boundary and writes it. Every line already received is
// routed (to a listener, or unclaimed) as received before the ask, so a
// waiting push is delivered rather than lost and is never the answer; a
// short drain window catches bytes already in flight. Then the line goes out
// and the reader resumes, routing what follows. A write failure answers the
// ask with the error and kills the connection.
func (r *reader) arm(ask lineAsk) bool {
	c := r.c
	r.carryPre = len(r.carry) > 0
	_ = c.c.SetReadDeadline(time.Now().Add(drainWindow))
	for {
		raw, err := c.br.ReadSlice('\n')
		if err != nil {
			if !isInterrupt(err) {
				ask.answer <- lineAnswer{err: err}
				r.die(err)
				return false
			}
			if !r.keep(raw) {
				return false
			}
			if len(r.carry) > 0 {
				r.carryPre = true // a line begun before the ask
			}
			break
		}
		if !r.keep(raw) {
			return false
		}
		r.carryPre = true
		r.complete() // armed is nil: routed to a listener or unclaimed
	}
	_ = c.c.SetReadDeadline(time.Time{})
	_ = c.c.SetWriteDeadline(time.Now().Add(ask.timeout))
	if _, err := fmt.Fprintf(c.c, "%s\r\n", ask.line); err != nil {
		ask.answer <- lineAnswer{err: fmt.Errorf("collection: line session send %s: %w", c.target, err)}
		c.fail(err)
		return false
	}
	r.armed = &ask
	return true
}

// isInterrupt reports a passed read deadline, the one timeout this
// connection makes itself (an exchange's interrupt, the drain window). A
// failed keepalive (ETIMEDOUT) is a real cause, not an interrupt.
func isInterrupt(err error) bool { return errors.Is(err, os.ErrDeadlineExceeded) }

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
// that does not arrive within the timeout kills the connection with
// ErrExchangeTimeout; an ask its caller abandons (a context deadline or
// cancel) is recycled too, but not as the device's timeout. Either way its
// reply may still be on the way, and on a held connection it would answer
// the next ask.
func (c *lineConn) Exchange(ctx context.Context, line string, reply *driver.Reply, timeout time.Duration) (string, error) {
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
	ask := lineAsk{line: line, reply: reply, timeout: timeout, answer: make(chan lineAnswer, 1), gone: make(chan struct{})}
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
		if a.refused {
			return "", &RefusedError{Request: line, Line: a.line}
		}
		return a.line, nil
	case <-c.done:
		return "", fmt.Errorf("collection: line session %s: %w", c.target, c.Err())
	case <-ctx.Done():
		if reply != nil {
			close(ask.gone) // shaped: the late answer cannot be misrouted
			return "", fmt.Errorf("collection: line session %s: %w", c.target, ctx.Err())
		}
		// The caller gave up (its deadline or a cancel), not the device:
		// recycled, since the reply may still come, but never recorded as
		// the device's timeout.
		c.fail(fmt.Errorf("exchange abandoned (%v); connection recycled", ctx.Err()))
		return "", fmt.Errorf("collection: line session %s: %w", c.target, ctx.Err())
	case <-t.C:
		if reply != nil {
			close(ask.gone)
			return "", fmt.Errorf("collection: line session %s: %q got no answer within %s: %w", c.target, line, timeout, ErrNoAnswer)
		}
		c.fail(ErrExchangeTimeout)
		return "", fmt.Errorf("collection: line session %s: no answer within %s: %w", c.target, timeout, ErrExchangeTimeout)
	}
}
