package collection

import (
	"bufio"
	"context"
	"fmt"
	"io"
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

// maxLineAnswer bounds one answer line so a device that never terminates cannot
// exhaust the node.
const maxLineAnswer = 64 * 1024

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
	deadline := time.Now().Add(timeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)
	if _, err := fmt.Fprintf(conn, "%s\r\n", line); err != nil {
		return "", fmt.Errorf("collection: line send %s: %w", target, err)
	}
	// Bound the answer: a device that never sends a newline would otherwise let
	// the read grow until the deadline (hundreds of MB on a fast LAN), and the
	// node is the whole collection agent for its placement.
	answer, err := bufio.NewReader(io.LimitReader(conn, maxLineAnswer)).ReadString('\n')
	if err != nil {
		if len(answer) >= maxLineAnswer {
			return "", fmt.Errorf("collection: line read %s: answer exceeded %d bytes with no terminator", target, maxLineAnswer)
		}
		return "", fmt.Errorf("collection: line read %s: %w", target, err)
	}
	return strings.TrimRight(answer, "\r\n"), nil
}
