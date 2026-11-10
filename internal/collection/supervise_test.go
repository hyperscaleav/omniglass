package collection_test

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// syncBuffer is a log sink safe for the goroutines a test supervises.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// captureLogs points the default logger at a buffer for the test. Not
// parallel: the default logger is the process's.
func captureLogs(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	prior := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prior) })
	return buf
}

func TestSuperviseRecoversAPanicIntoAnError(t *testing.T) {
	logs := captureLogs(t)
	before := collection.PanicsRecovered()
	pe := collection.Supervise("line reader 10.0.0.9:51325", func() { panic("index out of range") })
	if pe == nil {
		t.Fatal("a panic was not recovered")
	}
	if got := pe.Error(); got != "panic: index out of range" {
		t.Fatalf("error = %q, want the panic value behind panic:", got)
	}
	if pe.Where != "line reader 10.0.0.9:51325" || !strings.Contains(string(pe.Stack), "supervise_test.go") {
		t.Fatalf("where %q, stack %q: want the goroutine named and the panic's stack", pe.Where, pe.Stack)
	}
	if n := collection.PanicsRecovered() - before; n != 1 {
		t.Fatalf("counted %d panics, want 1", n)
	}
	out := logs.String()
	for _, want := range []string{"level=ERROR", "facility=collection", `where="line reader 10.0.0.9:51325"`, `panic="index out of range"`, "supervise_test.go"} {
		if !strings.Contains(out, want) {
			t.Errorf("log %q lacks %q", out, want)
		}
	}
	var asErr *collection.PanicError
	if !errors.As(error(pe), &asErr) {
		t.Fatal("a PanicError does not read as one through errors.As")
	}
}

func TestSuperviseReturnsNilWhenNothingPanics(t *testing.T) {
	logs := captureLogs(t)
	before := collection.PanicsRecovered()
	ran := false
	if pe := collection.Supervise("arm", func() { ran = true }); pe != nil {
		t.Fatalf("a clean return reported %v", pe)
	}
	if !ran || collection.PanicsRecovered() != before || logs.String() != "" {
		t.Fatalf("ran %v, count moved %d, logged %q", ran, collection.PanicsRecovered()-before, logs.String())
	}
}

// A panic carrying an error keeps it in the chain, so a caller can still
// tell what the value was.
func TestSupervisePanicWithAnErrorUnwrapsToIt(t *testing.T) {
	captureLogs(t)
	cause := errors.New("nil map write")
	pe := collection.Supervise("route", func() { panic(cause) })
	if !errors.Is(pe, cause) {
		t.Fatalf("%v does not unwrap to the panic's error", pe)
	}
}
