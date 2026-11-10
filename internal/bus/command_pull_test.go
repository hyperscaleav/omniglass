package bus

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// commandsOf is a store whose command queue read fails with err.
type commandsOf struct {
	fakeStore
	err error
}

func (s commandsOf) PendingNodeCommands(context.Context, string) ([]storage.CommandDelivery, error) {
	return nil, s.err
}

// warnLines is the WARN lines logged, each one carrying facility=command.
func warnLines(t *testing.T, logged string) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(logged), "\n") {
		if strings.Contains(line, "level=WARN") {
			if !strings.Contains(line, "facility=command") {
				t.Errorf("a WARN line without facility=command: %s", line)
			}
			out = append(out, line)
		}
	}
	return out
}

// A command pull whose queue read fails is logged once at WARN with the node
// and the error, and the node is sent nothing (it re-pulls next tick).
func TestAFailedCommandPullIsLogged(t *testing.T) {
	logged := capture(t, slog.LevelDebug)
	s := &Server{store: commandsOf{err: errors.New("pool exhausted")}}
	calls := 0
	s.answerCommands(context.Background(), "edge-1", func([]byte) error { calls++; return nil })

	if calls != 0 {
		t.Errorf("answered %d times, want no reply", calls)
	}
	lines := warnLines(t, logged.String())
	if len(lines) != 1 || !strings.Contains(lines[0], "node=edge-1") || !strings.Contains(lines[0], "pool exhausted") {
		t.Fatalf("logged %q, want one WARN line naming the node and the error", logged.String())
	}
}

// A reply the node never receives is logged at WARN with the node and the
// error.
func TestAnUnsentCommandPullReplyIsLogged(t *testing.T) {
	logged := capture(t, slog.LevelDebug)
	s := &Server{store: commandsOf{}}
	s.answerCommands(context.Background(), "edge-1", func([]byte) error { return errors.New("nats: connection closed") })

	lines := warnLines(t, logged.String())
	if len(lines) != 1 || !strings.Contains(lines[0], "node=edge-1") || !strings.Contains(lines[0], "connection closed") {
		t.Fatalf("logged %q, want one WARN line naming the node and the error", logged.String())
	}
}

// A status report the server cannot parse is logged at WARN with the node
// and the error, never silently dropped.
func TestAnUnparseableCommandStatusIsLogged(t *testing.T) {
	logged := capture(t, slog.LevelDebug)
	s := &Server{store: fakeStore{}}
	s.recordCommandStatus(context.Background(), "edge-1", []byte("{not json"))

	lines := warnLines(t, logged.String())
	if len(lines) != 1 || !strings.Contains(lines[0], "node=edge-1") {
		t.Fatalf("logged %q, want one WARN line naming the node", logged.String())
	}
}
