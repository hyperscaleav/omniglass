package collection

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The datagram primitive (#1037): an endpoint's udp socket. The line
// protocol rides it as datagrams, one line each: an exchange sends the
// request and takes the reply by its declared shape, a lost datagram is
// re-sent within the one timeout, and a datagram from the component that
// no exchange takes is offered to the endpoint's listeners. Each endpoint
// binds its own local port (the driver's listen-port input, else one the
// kernel picks) and its requests leave from it, so a reply and a push
// arrive on one socket with no table of sources (decision 5 of #1076).
//
// The socket is not connected: a connected udp socket filters other
// sources in the kernel, and a datagram from another source must be
// counted, not silently lost. The host's port-unreachable report still
// reaches the exchange (refused, not a timeout) through the socket's error
// queue, which the platform file turns on (IP_RECVERR on Linux; elsewhere a
// closed port reads as a timeout).

// SignalUDPResponseTime is the udp request rung's timing, one observation
// per answered probe (decision 7a of #1076; folded into rtt by #1063).
const SignalUDPResponseTime = probekeys.UDPResponseTime

// DatagramConfig is what an endpoint's socket is made from.
type DatagramConfig struct {
	// Target is the component's host:port.
	Target string
	// LocalPort is the port to bind; zero lets the kernel pick one.
	LocalPort int
	// Inbound hears every line from the component no exchange takes (nil
	// drops them).
	Inbound Inbound
	// Terminal classifies an answer, or the component's refusal, as a
	// refusal retrying cannot fix (the driver's terminal rules).
	Terminal Classifier
}

// DatagramAnswer is an answered exchange: the reply, the time from the send
// it answered, and how many sends the exchange made.
type DatagramAnswer struct {
	Line     string
	RTT      time.Duration
	Attempts int
}

// DatagramExchanger is the slice of an endpoint's socket a poll, a command
// or the request rung borrows. *DatagramConn satisfies it.
type DatagramExchanger interface {
	Exchange(ctx context.Context, line string, reply *driver.Reply, timeout time.Duration, retries int) (DatagramAnswer, error)
}

// maxDatagram bounds one datagram read; a text reply fits well within it.
const maxDatagram = 64 * 1024

// datagramAsk is the exchange awaiting its reply.
type datagramAsk struct {
	reply  *driver.Reply
	answer chan lineAnswer
}

// datagramGhost is an answered exchange whose surplus replies may still be
// on the way: each send a slow component answers draws a reply, so an
// exchange settled on its third send can leave two behind. Until they are
// absorbed, or the exchange's timeout has passed again, a line repeating its
// answer is that surplus, never the next ask's answer. Only the repeat is
// absorbed, so a next ask whose genuine answer differs (a command's new
// value) is never starved; one whose answer is the same line loses at most a
// share of its timeout to a retry. An exchange no send of which was answered
// leaves no ghost: on a lossy wire its sends drew no replies, and a ghost
// absorbing by shape would starve the asks after it. A reply later than the
// whole timeout is therefore not told from the next ask's (the endpoint's
// timeout should exceed the component's latency), which is why every udp
// request declares its reply's shape.
type datagramGhost struct {
	answer    string
	remaining int
	until     time.Time
}

// absorbs reports a line that is the ghost's surplus.
func (g *datagramGhost) absorbs(line string, now time.Time) bool {
	return g != nil && g.remaining > 0 && !now.After(g.until) && line == g.answer
}

// DatagramConn is one endpoint's bound socket.
type DatagramConn struct {
	conn *net.UDPConn
	peer *net.UDPAddr
	cfg  DatagramConfig
	xmu  sync.Mutex // one exchange at a time

	mu          sync.Mutex
	armed       *datagramAsk
	ghost       *datagramGhost
	foreign     int
	lastForeign string
	err         error

	done      chan struct{}
	closeOnce sync.Once
}

// ListenDatagram binds an endpoint's socket and starts its reader.
func ListenDatagram(cfg DatagramConfig) (*DatagramConn, error) {
	peer, err := net.ResolveUDPAddr("udp", cfg.Target)
	if err != nil {
		return nil, fmt.Errorf("collection: udp target %s: %w", cfg.Target, err)
	}
	network := "udp4"
	if peer.IP.To4() == nil {
		network = "udp6"
	}
	conn, err := net.ListenUDP(network, &net.UDPAddr{Port: cfg.LocalPort})
	if err != nil {
		return nil, fmt.Errorf("collection: udp bind for %s: %w", cfg.Target, err)
	}
	if err := enableErrorQueue(conn, network == "udp6"); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("collection: udp socket for %s: %w", cfg.Target, err)
	}
	c := &DatagramConn{conn: conn, peer: peer, cfg: cfg, done: make(chan struct{})}
	go c.read()
	return c, nil
}

// LocalAddr is the address the socket bound.
func (c *DatagramConn) LocalAddr() net.Addr { return c.conn.LocalAddr() }

// Done is closed once the socket is closed or its reader failed.
func (c *DatagramConn) Done() <-chan struct{} { return c.done }

// Close closes the socket; a later exchange fails.
func (c *DatagramConn) Close() error {
	c.fail(net.ErrClosed)
	return nil
}

func (c *DatagramConn) fail(err error) {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.err = err
		c.mu.Unlock()
		_ = c.conn.Close()
		close(c.done)
	})
}

func (c *DatagramConn) cause() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// ForeignCount is how many datagrams from another source were dropped since
// the last drain.
func (c *DatagramConn) ForeignCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.foreign
}

// DrainForeign returns and resets the count of datagrams from another source
// since the last drain, with the last such source.
func (c *DatagramConn) DrainForeign() (n int, last string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, last = c.foreign, c.lastForeign
	c.foreign, c.lastForeign = 0, ""
	return n, last
}

// fromPeer reports a datagram from the component itself: its address and
// its port.
func (c *DatagramConn) fromPeer(from *net.UDPAddr) bool {
	return from != nil && from.Port == c.peer.Port && from.IP.Equal(c.peer.IP)
}

// read is the reader goroutine: every datagram from the component is split
// into its lines and routed; one from another source is counted and
// dropped; the host's report of an unreachable port answers the exchange
// in flight.
func (c *DatagramConn) read() {
	buf := make([]byte, maxDatagram)
	for {
		n, from, err := c.conn.ReadFromUDP(buf)
		if err != nil {
			var errno syscall.Errno
			if errors.As(err, &errno) {
				// The error queue's report of an earlier send: consumed, so
				// it neither repeats nor holds the receive buffer.
				drainErrorQueue(c.conn)
				c.answer(nil, lineAnswer{err: fmt.Errorf("collection: udp %s: %w", c.cfg.Target, err)})
				continue
			}
			c.fail(err)
			return
		}
		if !c.fromPeer(from) {
			c.mu.Lock()
			c.foreign++
			c.lastForeign = from.String()
			c.mu.Unlock()
			continue
		}
		for _, line := range strings.Split(string(buf[:n]), "\n") {
			if line = trimLine(line); line != "" {
				c.route(line)
			}
		}
	}
}

// route hands a line from the component to the exchange in flight or the
// listeners, with the held line connection's precedence (#817): a line a
// listener claims is the listener's, and the exchange has it too only when
// its declared answer shape matches; with no shape declared, the first line
// no listener claims answers; an unclaimed line matching the error shape
// answers as the component's refusal; whatever is left is unclaimed.
func (c *DatagramConn) route(line string) {
	claimed := c.cfg.Inbound != nil && c.cfg.Inbound.Claim(line)
	c.mu.Lock()
	if g := c.ghost; !claimed && g.absorbs(line, time.Now()) {
		g.remaining--
		c.mu.Unlock()
		return // a surplus reply to an exchange already settled
	}
	a := c.armed
	c.mu.Unlock()
	if a != nil {
		switch {
		case (a.reply != nil && a.reply.Accepts(line)) || (a.reply == nil && !claimed):
			if c.answer(a, lineAnswer{line: line}) {
				return
			}
		case !claimed && a.reply.Fails(line):
			if c.answer(a, lineAnswer{line: line, refused: true}) {
				return
			}
		}
	}
	if !claimed && c.cfg.Inbound != nil {
		c.cfg.Inbound.Unclaimed(line)
	}
}

// answer hands the armed exchange its outcome, once. want names the ask the
// line was judged against (nil: whichever is armed). false when that ask is
// no longer armed.
func (c *DatagramConn) answer(want *datagramAsk, ans lineAnswer) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	a := c.armed
	if a == nil || (want != nil && a != want) {
		return false
	}
	c.armed = nil
	a.answer <- ans // buffered, and disarmed: never blocks
	return true
}

// Exchange sends one line and waits for its reply, re-sending it up to
// retries more times; the attempts share the timeout equally, so a lost
// datagram costs its share and the exchange never outlasts the timeout. A
// reply matching the error shape fails it with the component's text, and
// one the driver declares terminal fails it with a refusal retrying cannot
// fix; neither is retried, nor is the host's report of an unreachable port.
func (c *DatagramConn) Exchange(ctx context.Context, line string, reply *driver.Reply, timeout time.Duration, retries int) (DatagramAnswer, error) {
	if timeout <= 0 {
		timeout = defaultSessionTimeout
	}
	if retries < 0 {
		retries = 0
	}
	c.xmu.Lock()
	defer c.xmu.Unlock()
	select {
	case <-c.done:
		return DatagramAnswer{}, fmt.Errorf("collection: udp %s: %w", c.cfg.Target, c.cause())
	default:
	}
	ask := &datagramAsk{reply: reply, answer: make(chan lineAnswer, 1)}
	c.mu.Lock()
	c.armed = ask
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if c.armed == ask {
			c.armed = nil
		}
		c.mu.Unlock()
	}()
	share := timeout / time.Duration(retries+1)
	payload := []byte(line + "\r\n")
	first := time.Now()
	for attempt := 1; attempt <= retries+1; attempt++ {
		if _, err := c.conn.WriteToUDP(payload, c.peer); err != nil {
			return DatagramAnswer{}, fmt.Errorf("collection: udp send %s: %w", c.cfg.Target, err)
		}
		t := time.NewTimer(share)
		select {
		case a := <-ask.answer:
			t.Stop()
			if a.err == nil && attempt > 1 {
				c.haunt(&datagramGhost{answer: a.line, remaining: attempt - 1}, timeout)
			}
			// Timed from the first send: a reply may answer any send, so
			// the time is what the exchange took, a lost datagram's retry
			// included, and never less than the component's latency.
			return c.settle(line, a, DatagramAnswer{Line: a.line, RTT: time.Since(first), Attempts: attempt})
		case <-c.done:
			t.Stop()
			return DatagramAnswer{}, fmt.Errorf("collection: udp %s: %w", c.cfg.Target, c.cause())
		case <-ctx.Done():
			t.Stop()
			return DatagramAnswer{}, fmt.Errorf("collection: udp %s: %w", c.cfg.Target, ctx.Err())
		case <-t.C:
		}
	}
	return DatagramAnswer{}, &datagramTimeout{target: c.cfg.Target, line: line, timeout: timeout, attempts: retries + 1}
}

// haunt leaves an ended exchange's ghost for one more timeout.
func (c *DatagramConn) haunt(g *datagramGhost, timeout time.Duration) {
	g.until = time.Now().Add(timeout)
	c.mu.Lock()
	c.ghost = g
	c.mu.Unlock()
}

// settle turns a delivered outcome into the exchange's result: an error
// (the host's report), the component's refusal, or the answer, each
// classified by the driver's terminal rules first.
func (c *DatagramConn) settle(line string, a lineAnswer, ans DatagramAnswer) (DatagramAnswer, error) {
	if a.err != nil {
		return DatagramAnswer{}, a.err
	}
	if terr := c.cfg.Terminal.classify(a.line); terr != nil {
		return DatagramAnswer{}, terr
	}
	if a.refused {
		return DatagramAnswer{}, &RefusedError{Request: line, Line: a.line}
	}
	return ans, nil
}

// datagramTimeout is an exchange no attempt of which was answered: a
// timeout (net.Error), and ErrNoAnswer.
type datagramTimeout struct {
	target   string
	line     string
	timeout  time.Duration
	attempts int
}

func (e *datagramTimeout) Error() string {
	return fmt.Sprintf("collection: udp %s: %q got no answer within %s (%d sent): %v", e.target, e.line, e.timeout, e.attempts, ErrNoAnswer)
}
func (e *datagramTimeout) Unwrap() error   { return ErrNoAnswer }
func (e *datagramTimeout) Timeout() bool   { return true }
func (e *datagramTimeout) Temporary() bool { return true }
