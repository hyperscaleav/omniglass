package collection

import (
	"errors"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// TerminalError is a refusal retrying cannot fix (#1049): a wrong
// credential, a changed host key or certificate, a protocol the device will
// not speak. Class is one of the driver's terminal classes (driver.ClassAuth
// and siblings); Reason is the device's own text, or the transport's account
// of the refusal. A held session that meets one reads rejected and stops
// dialing; anything unclassified stays retryable. Reason never carries a
// credential.
type TerminalError struct {
	Class  string
	Reason string
	// Pinned and Offered are set on a refusal of a changed key (an ssh host
	// key, #1078; a tls certificate's public key, #1035): the key the
	// endpoint pins and the key the component now offers, each a SHA256
	// fingerprint, carried as data so no surface parses them out of Reason.
	// Empty on every other refusal.
	Pinned  string
	Offered string
}

func (e *TerminalError) Error() string { return e.Class + ": " + e.Reason }

// HostKeyRefusal is the refusal of an ssh host key that differs from the pin
// (#1032): class host-key, both keys as data (#1078), and a reason naming
// them for the property's series and the CLI reader. The held session's
// dialer and the probe both refuse with it.
func HostKeyRefusal(pinned, offered string) *TerminalError {
	return &TerminalError{Class: driver.ClassHostKey, Pinned: pinned, Offered: offered, Reason: fmt.Sprintf(
		"the host key changed: pinned %s, offered %s; retry the endpoint with the offered key to trust it", pinned, offered)}
}

// AsTerminal reports whether err is, or wraps, a TerminalError.
func AsTerminal(err error) (*TerminalError, bool) {
	var t *TerminalError
	if errors.As(err, &t) {
		return t, true
	}
	return nil, false
}

// Classifier classifies a line from the device as terminal: the driver's
// declared terminal rules (driver.SessionSpec.TerminalFor). Nil classifies
// nothing.
type Classifier func(line string) (class string, ok bool)

// classify applies a classifier to a line, yielding the TerminalError it
// names.
func (c Classifier) classify(line string) *TerminalError {
	if c == nil {
		return nil
	}
	if class, ok := c(line); ok {
		return &TerminalError{Class: class, Reason: line}
	}
	return nil
}

// TerminalClassifier adapts a driver's session block into a Classifier; nil
// for a driver that declares none.
func TerminalClassifier(s *driver.SessionSpec) Classifier {
	if s == nil || len(s.Terminal) == 0 {
		return nil
	}
	return s.TerminalFor
}
