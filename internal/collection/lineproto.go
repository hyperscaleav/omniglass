package collection

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

// The stateless line-protocol client (#814): one ask over a throwaway TCP
// connection (dial, send the request line, read the answer line), which is how
// a line-protocol driver's poll functions run until the stateful arc gives the
// transport a held session to borrow.

// LineExchanger sends one line and returns the one-line answer, both without
// their terminators.
type LineExchanger interface {
	Exchange(ctx context.Context, target, line string, timeout time.Duration) (string, error)
}

// NewLineExchanger returns the real dialer-backed exchanger.
func NewLineExchanger() LineExchanger { return &lineExchanger{} }

type lineExchanger struct{}

func (e *lineExchanger) Exchange(ctx context.Context, target, line string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", target)
	if err != nil {
		return "", fmt.Errorf("collection: line dial %s: %w", target, err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := fmt.Fprintf(conn, "%s\r\n", line); err != nil {
		return "", fmt.Errorf("collection: line send %s: %w", target, err)
	}
	answer, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("collection: line read %s: %w", target, err)
	}
	return strings.TrimRight(answer, "\r\n"), nil
}
