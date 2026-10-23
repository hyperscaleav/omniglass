package collection

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"syscall"
	"time"
)

// Telnet (#1033): the line protocol over TCP plus Telnet's option
// negotiation. The client refuses every option: inbound IAC sequences are
// stripped from the stream (so the login dialog and the line reader see only
// data), DO is answered WONT and WILL is answered DONT, and a literal 0xFF
// the session writes is escaped as IAC IAC. The held session, its login and
// its reader are the line protocol's own; telnet is only this filter.

const (
	telnetIAC  = 0xFF
	telnetDONT = 0xFE
	telnetDO   = 0xFD
	telnetWONT = 0xFC
	telnetWILL = 0xFB
	telnetSB   = 0xFA
	telnetSE   = 0xF0
)

// telnetReplyTimeout bounds a negotiation reply's write.
const telnetReplyTimeout = 5 * time.Second

// telnetState is where the inbound filter is inside a command sequence.
type telnetState int

const (
	tsData   telnetState = iota
	tsIAC                // read IAC
	tsOption             // read IAC and a verb that takes an option (DO/DONT/WILL/WONT)
	tsSub                // inside a subnegotiation
	tsSubIAC             // read IAC inside a subnegotiation
)

// telnetConn filters a TCP connection's telnet commands. Read strips them
// (state carried across reads, so a sequence split across segments is still
// stripped) and answers each negotiation with a refusal; Write escapes 0xFF.
// Deadlines pass through to the connection.
type telnetConn struct {
	net.Conn
	wmu   sync.Mutex // negotiation replies and session writes share the wire
	state telnetState
	verb  byte
}

// NewTelnetConn wraps a connection in the telnet filter.
func NewTelnetConn(c net.Conn) net.Conn { return &telnetConn{Conn: c} }

func (t *telnetConn) Read(p []byte) (int, error) {
	for {
		n, err := t.Conn.Read(p)
		out, reply := t.filter(p[:n])
		if len(reply) > 0 {
			// The reply carries its own deadline: the session's last write
			// deadline may have passed long ago on an idle connection. A reply
			// that cannot be written is not this read's error (a timeout here
			// would pass for the line reader's own interrupt); a dead
			// connection fails the next read on its own.
			t.wmu.Lock()
			_ = t.Conn.SetWriteDeadline(time.Now().Add(telnetReplyTimeout))
			_, _ = t.Conn.Write(reply)
			_ = t.Conn.SetWriteDeadline(time.Time{})
			t.wmu.Unlock()
		}
		// A read that carried only commands returns nothing to the caller:
		// read again rather than hand back an empty, error-free read.
		if out > 0 || err != nil {
			return out, err
		}
	}
}

// filter strips telnet commands from buf in place, returning the data length
// and the negotiation replies owed.
func (t *telnetConn) filter(buf []byte) (int, []byte) {
	out := 0
	var reply []byte
	for _, b := range buf {
		switch t.state {
		case tsData:
			if b == telnetIAC {
				t.state = tsIAC
				continue
			}
			buf[out] = b
			out++
		case tsIAC:
			switch b {
			case telnetIAC: // an escaped 0xFF is data
				buf[out] = b
				out++
				t.state = tsData
			case telnetDO, telnetDONT, telnetWILL, telnetWONT:
				t.verb, t.state = b, tsOption
			case telnetSB:
				t.state = tsSub
			default: // a two-byte command (NOP, GA, ...): nothing to answer
				t.state = tsData
			}
		case tsOption:
			switch t.verb {
			case telnetDO:
				reply = append(reply, telnetIAC, telnetWONT, b)
			case telnetWILL:
				reply = append(reply, telnetIAC, telnetDONT, b)
			}
			// DONT and WONT confirm what is already the case: answering
			// them would loop.
			t.state = tsData
		case tsSub:
			if b == telnetIAC {
				t.state = tsSubIAC
			}
		case tsSubIAC:
			if b == telnetSE {
				t.state = tsData
			} else {
				t.state = tsSub
			}
		}
	}
	return out, reply
}

func (t *telnetConn) Write(p []byte) (int, error) {
	escaped := make([]byte, 0, len(p))
	for _, b := range p {
		escaped = append(escaped, b)
		if b == telnetIAC {
			escaped = append(escaped, telnetIAC)
		}
	}
	t.wmu.Lock()
	defer t.wmu.Unlock()
	if _, err := t.Conn.Write(escaped); err != nil {
		return 0, err
	}
	return len(p), nil
}

// NewTelnetSessionDialer returns the held telnet dialer: the line session's
// staged dialer with the telnet filter on every connection.
func NewTelnetSessionDialer() StagedDialer { return lineSessionDialer{telnet: true} }

// TelnetProbe is the telnet reachability probe (#1033): connect, then wait
// for the first bytes the component sends (a negotiation or a banner). A
// port that accepts and stays silent is reached but not responsive.
type TelnetProbe interface {
	Probe(ctx context.Context, target string, timeout time.Duration) (TelnetResult, error)
}

// TelnetResult is one telnet probe observation, the ladder's first two rungs.
type TelnetResult struct {
	Dialed     bool
	Responded  bool
	DialMS     float64
	ResponseMS float64
	Reason     Reachability
}

// NewTelnetProber returns the real telnet probe.
func NewTelnetProber() TelnetProbe { return telnetProber{} }

type telnetProber struct{}

func (telnetProber) Probe(ctx context.Context, target string, timeout time.Duration) (TelnetResult, error) {
	if timeout <= 0 {
		timeout = defaultTCPTimeout
	}
	var res TelnetResult
	start := time.Now()
	d := net.Dialer{Timeout: timeout}
	c, err := d.DialContext(ctx, "tcp", target)
	if err != nil {
		if reason, ok := Classify(err); ok {
			res.Reason = reason
			return res, nil
		}
		return TelnetResult{}, fmt.Errorf("collection: telnet probe %s: %w", target, err)
	}
	defer func() { _ = c.Close() }()
	res.Dialed = true
	res.DialMS = float64(time.Since(start)) / float64(time.Millisecond)
	_ = c.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 1)
	if n, err := c.Read(buf); n == 0 {
		res.Reason = Timedout
		switch {
		case errors.Is(err, io.EOF), errors.Is(err, syscall.ECONNRESET):
			// Accepted, then hung up before a byte: a session limit or an
			// ACL, not a slow component.
			res.Reason = Closed
		default:
			if reason, ok := Classify(err); ok {
				res.Reason = reason
			}
		}
		return res, nil
	}
	res.Responded = true
	res.ResponseMS = float64(time.Since(start)) / float64(time.Millisecond)
	res.Reason = Responded
	return res, nil
}
