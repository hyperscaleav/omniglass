package collection

import (
	"context"
	"net"
	"time"
)

// TCPDialer is the TCP-connect probe boundary, faked in unit tests so collection
// logic is hermetic (no real sockets). A failed connect is DATA, not an error:
// the reason (Refused / Timedout / Prohibited / Unreachable) classifies why, and
// reach.Up() is the open verdict. err is reserved for a target that could not be
// attempted at all (an unresolved host), which the caller treats as inconclusive.
type TCPDialer interface {
	Dial(ctx context.Context, addr string, timeout time.Duration) (connectMS float64, reach Reachability, err error)
}

// NewTCPDialer returns the real TCP-connect probe. A connect verdict is data, not
// an error, and is classified: a RST reads Refused, an admin-prohibited path (the
// kernel surfaces it to connect() as EHOSTUNREACH/EPERM) reads Unreachable or
// Prohibited, and a silent drop reads Timedout. A resolve/setup failure (no usable
// address) returns a non-nil err: the caller treats it as inconclusive, since it
// says nothing about the target.
func NewTCPDialer() TCPDialer { return tcpDialer{} }

type tcpDialer struct{}

func (tcpDialer) Dial(ctx context.Context, addr string, timeout time.Duration) (float64, Reachability, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	d := net.Dialer{Timeout: timeout}
	start := time.Now()
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		if reason, ok := Classify(err); ok {
			return 0, reason, nil
		}
		return 0, "", err // resolve/setup failure: inconclusive, not a port verdict
	}
	elapsed := float64(time.Since(start)) / float64(time.Millisecond)
	_ = conn.Close()
	return elapsed, Responded, nil
}
