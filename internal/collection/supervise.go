package collection

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync/atomic"
)

// PanicError is a panic recovered on a session-layer goroutine, made an
// error: the session it ran under fails it as it fails any other cause
// (#1636), so one device's bad bytes cost that device's session, never the
// node.
type PanicError struct {
	// Where names the goroutine that panicked ("line reader 10.0.0.9:51325").
	Where string
	// Value is what was passed to panic.
	Value any
	// Stack is the panicking goroutine's stack, as logged.
	Stack []byte
}

func (e *PanicError) Error() string { return fmt.Sprintf("panic: %v", e.Value) }

// Unwrap keeps a panic's error value in the chain.
func (e *PanicError) Unwrap() error {
	err, _ := e.Value.(error)
	return err
}

var panicsRecovered atomic.Int64

// PanicsRecovered is how many panics Supervise has recovered in this
// process.
func PanicsRecovered() int64 { return panicsRecovered.Load() }

// Supervise runs fn and recovers a panic out of it into a PanicError,
// logging it with its stack under the collection facility and counting it;
// nil when fn returns. Every goroutine the session layer starts runs under
// it, and the caller fails the session it belongs to with the result.
func Supervise(where string, fn func()) (pe *PanicError) {
	defer func() {
		v := recover()
		if v == nil {
			return
		}
		pe = &PanicError{Where: where, Value: v, Stack: debug.Stack()}
		n := panicsRecovered.Add(1)
		slog.Error("panic recovered", "facility", "collection", "where", where,
			"panic", fmt.Sprint(v), "stack", string(pe.Stack), "count", n)
	}()
	fn()
	return nil
}

// asPanic reports a recovered panic anywhere in err's chain.
func asPanic(err error) (*PanicError, bool) {
	var pe *PanicError
	return pe, errors.As(err, &pe)
}

// DialSlots bounds how many sessions dial at once, node-wide: a node
// restarted with every held endpoint wanted brings them up a few at a time
// rather than all at once. A session waiting for a slot reads establishing.
// Nil is unbounded.
type DialSlots chan struct{}

// NewDialSlots makes n slots; n <= 0 is unbounded (nil).
func NewDialSlots(n int) DialSlots {
	if n <= 0 {
		return nil
	}
	return make(DialSlots, n)
}

// acquire takes a slot, or reports false when ctx ends first.
func (d DialSlots) acquire(ctx context.Context) bool {
	if d == nil {
		return ctx.Err() == nil
	}
	select {
	case d <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (d DialSlots) release() {
	if d != nil {
		<-d
	}
}

// goSupervised starts fn on its own goroutine under Supervise, handing a
// recovered panic to onPanic (which fails the connection it belonged to).
func goSupervised(where string, fn func(), onPanic func(*PanicError)) {
	go func() {
		if pe := Supervise(where, fn); pe != nil {
			onPanic(pe)
		}
	}()
}
