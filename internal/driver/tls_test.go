package driver_test

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The line protocol over tls (#1035): a driver on the tls transport polls,
// listens and actuates over lines like one on tcp, its ladder's transport
// rung is the handshake, and its verification mode is a driver input
// (decision 3 of #1076): the driver declares its default, a driver that
// declares none verifies, and skip is never a default.

func tlsSpec() *driver.Spec {
	s := validSpec()
	s.Transport = "tls"
	s.Inputs = []driver.Input{
		{Name: "host", Kind: "string", Required: true},
		{Name: "port", Kind: "number", Default: "51326"},
		{Name: driver.TLSModeInput, Kind: "string", Default: driver.TLSModePin},
	}
	return s
}

func TestATLSDriverValidates(t *testing.T) {
	if err := tlsSpec().Validate(catalog()); err != nil {
		t.Fatalf("a tls line driver refused: %v", err)
	}
	// A tls driver need declare no mode at all: it verifies.
	s := tlsSpec()
	s.Inputs = s.Inputs[:2]
	if err := s.Validate(catalog()); err != nil {
		t.Fatalf("a tls driver declaring no mode refused: %v", err)
	}
}

func TestTLSLadderDefaultsToTheHandshake(t *testing.T) {
	l := driver.ResolveLadder("tls", nil, nil)
	if l.Transport != driver.RungHandshake || l.Session || !l.Ping {
		t.Fatalf("tls resolved %+v, want the handshake, no session, ping on", l)
	}
	// The handshake can be stepped down to a bare connect, never up to a
	// request.
	s := tlsSpec()
	s.Ladder = &driver.Ladder{Transport: &driver.TransportRung{Rung: driver.RungConnect}}
	if err := s.Validate(catalog()); err != nil {
		t.Fatalf("a tls connect rung refused: %v", err)
	}
	s.Ladder = &driver.Ladder{Transport: &driver.TransportRung{Rung: driver.RungRequest, Request: &driver.Request{Line: "GET INPUT"}}}
	if err := s.Validate(catalog()); err == nil || !strings.Contains(err.Error(), "request") {
		t.Fatalf("a tls request rung = %v, want refused", err)
	}
}

func TestTLSModeInputRefusals(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(s *driver.Spec)
		want   string
	}{
		{"skip is never a default", func(s *driver.Spec) { s.Inputs[2].Default = driver.TLSModeSkip }, "never a default"},
		{"an unknown mode", func(s *driver.Spec) { s.Inputs[2].Default = "trust-me" }, "verify, pin, skip"},
		{"a number", func(s *driver.Spec) { s.Inputs[2].Kind = "number"; s.Inputs[2].Default = "" }, "string"},
		{"a secret", func(s *driver.Spec) {
			s.Inputs[2].Kind = "secret"
			s.Inputs[2].SecretType = "basic-auth"
			s.Inputs[2].Default = ""
		}, "string"},
		{"on a transport with no tls", func(s *driver.Spec) { s.Transport = "tcp" }, "tls"},
	}
	for _, tc := range cases {
		s := tlsSpec()
		tc.mutate(s)
		if err := s.Validate(catalog()); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want a refusal naming %q", tc.name, err, tc.want)
		}
	}
}

func TestParseTLSMode(t *testing.T) {
	for in, want := range map[string]string{"": driver.TLSModeVerify, "verify": driver.TLSModeVerify, "pin": driver.TLSModePin, "skip": driver.TLSModeSkip} {
		got, err := driver.ParseTLSMode(in)
		if err != nil || got != want {
			t.Errorf("ParseTLSMode(%q) = %q, %v, want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"Pin", "none", "insecure"} {
		if _, err := driver.ParseTLSMode(bad); err == nil {
			t.Errorf("ParseTLSMode(%q) accepted", bad)
		}
	}
}

// An attach or an in-place edit sets an input's value; the one the engine
// reads as the tls mode must be a mode.
func TestCheckInputValue(t *testing.T) {
	s := tlsSpec()
	if err := s.CheckInputValue(driver.TLSModeInput, "skip"); err != nil {
		t.Fatalf("skip, chosen by the operator, refused: %v", err)
	}
	if err := s.CheckInputValue(driver.TLSModeInput, "maybe"); err == nil || !strings.Contains(err.Error(), "verify, pin, skip") {
		t.Fatalf("an unknown mode = %v, want refused naming the modes", err)
	}
	if err := s.CheckInputValue("host", "anything"); err != nil {
		t.Fatalf("a plain input refused: %v", err)
	}
}

// Review of #1035, finding 1: a key the node publishes to steer its own
// attempts (a pin, the gate, a held session's account of itself) is the
// node's alone. A driver emit named after one would land as the observed
// value through the poll path, so the worklist would deliver a driver's
// constant as the pin, bypassing the audited :retry. Refused at write.
func TestADriverMayNotEmitANodeControlKey(t *testing.T) {
	for _, key := range []string{"tls-certificate", "ssh-host-key", "endpoint-rejection", "session-state", "session-reason"} {
		cat := catalog()
		cat.lanes[key] = "property"
		s := tlsSpec()
		s.Polls[0].Emits = append(s.Polls[0].Emits, driver.Emit{Name: key, Extract: driver.Extract{Regex: `^INPUT (\S+)$`}})
		if err := s.Validate(cat); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("a poll emitting %s = %v, want refused naming it", key, err)
		}
		s = tlsSpec()
		s.Listeners[0].Emits = append(s.Listeners[0].Emits, driver.Emit{Name: key, Extract: driver.Extract{Regex: `^EVT (\S+)$`}})
		if err := s.Validate(cat); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("a listener emitting %s = %v, want refused naming it", key, err)
		}
	}
}
