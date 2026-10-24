package driver_test

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// Datagrams over udp (#1037): a udp driver polls, listens and actuates with
// lines carried one per datagram; its transport rung is a named request
// answered, which it must declare (an open udp port cannot be detected); a
// request may declare its retries (udp only); its listen port is the input
// listen-port (decision 5 of #1076); and its session block may declare only
// terminal replies, since nothing logs in over udp.

func udpSpec() *driver.Spec {
	s := validSpec()
	s.Transport = "udp"
	s.Inputs = []driver.Input{
		{Name: "host", Kind: "string", Required: true},
		{Name: "port", Kind: "number", Default: "51327"},
		{Name: driver.ListenPortInput, Kind: "number"},
	}
	s.Ladder = &driver.Ladder{Transport: &driver.TransportRung{Request: &driver.Request{Line: "GET INPUT", Reply: &driver.Reply{Prefix: "INPUT "}}}}
	s.Polls[0].Request.Reply = &driver.Reply{Prefix: "STATUS "}
	s.Commands[0].Request.Reply = &driver.Reply{Prefix: "INPUT "}
	return s
}

func intp(n int) *int { return &n }

func TestAUDPDriverValidates(t *testing.T) {
	if err := udpSpec().Validate(catalog()); err != nil {
		t.Fatalf("a udp driver refused: %v", err)
	}
	s := udpSpec()
	s.Polls[0].Request.Retries = intp(3)
	s.Commands[0].Request.Retries = intp(1)
	s.Ladder.Transport.Request.Retries = intp(0)
	s.Session = &driver.SessionSpec{Terminal: []driver.TerminalRule{{Class: driver.ClassProtocol, Match: driver.Match{Prefix: "-ERR LOCKED"}}}}
	if err := s.Validate(catalog()); err != nil {
		t.Fatalf("a udp driver declaring retries and a terminal reply refused: %v", err)
	}
}

func TestUDPRefusals(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(s *driver.Spec)
		want   string
	}{
		{"no request rung", func(s *driver.Spec) { s.Ladder = nil }, "ladder.transport.request"},
		{"a request rung with no line", func(s *driver.Spec) { s.Ladder.Transport.Request = &driver.Request{Path: "/x"} }, "line"},
		{"a poll with no line", func(s *driver.Spec) {
			s.Polls[0].Request = driver.Request{Get: []string{"1.3"}, Reply: &driver.Reply{Prefix: "X"}}
		}, "line"},
		{"too many retries", func(s *driver.Spec) { s.Polls[0].Request.Retries = intp(9) }, "retries"},
		{"negative retries", func(s *driver.Spec) { s.Polls[0].Request.Retries = intp(-1) }, "retries"},
		{"a login", func(s *driver.Spec) {
			s.Inputs = append(s.Inputs, driver.Input{Name: "login", Kind: "secret", SecretType: "basic-auth"})
			s.Session = &driver.SessionSpec{Login: &driver.Login{Steps: []driver.LoginStep{{Expect: driver.Match{Prefix: "login:"}, Send: "${secret.login.username}"}}, Ready: driver.Match{Prefix: "R"}}}
		}, "terminal"},
		{"a re-probe", func(s *driver.Spec) { s.Session = &driver.SessionSpec{Reprobe: "1h"} }, "terminal"},
		{"a listen port that is not a number", func(s *driver.Spec) { s.Inputs[2].Kind = "string" }, driver.ListenPortInput},
		{"a listen port default out of range", func(s *driver.Spec) { s.Inputs[2].Default = "70000" }, driver.ListenPortInput},
		// Over udp a reply of any shape could be a late one to an earlier
		// ask, so every request declares the shape of its answer (review of
		// #1037, finding 2).
		{"a poll with no reply shape", func(s *driver.Spec) { s.Polls[0].Request.Reply = nil }, "reply"},
		{"a command with no reply shape", func(s *driver.Spec) { s.Commands[0].Request.Reply = nil }, "reply"},
		{"a request rung with no reply shape", func(s *driver.Spec) { s.Ladder.Transport.Request.Reply = nil }, "reply"},
	}
	for _, tc := range cases {
		s := udpSpec()
		tc.mutate(s)
		if err := s.Validate(catalog()); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want a refusal naming %q", tc.name, err, tc.want)
		}
	}
}

// Retries are a datagram's; a line over a held session or a tcp exchange
// retries nothing, so declaring them there is a mistake refused at write.
// The listen port is udp's alone too.
func TestRetriesAndTheListenPortAreUDPs(t *testing.T) {
	s := validSpec()
	s.Polls[0].Request.Retries = intp(2)
	if err := s.Validate(catalog()); err == nil || !strings.Contains(err.Error(), "retries") {
		t.Fatalf("retries on a tcp poll = %v, want refused", err)
	}
	s = validSpec()
	s.Inputs = append(s.Inputs, driver.Input{Name: driver.ListenPortInput, Kind: "number"})
	if err := s.Validate(catalog()); err == nil || !strings.Contains(err.Error(), driver.ListenPortInput) {
		t.Fatalf("a listen port on a tcp driver = %v, want refused", err)
	}
}

// Retries default by family: 2 for a poll and the request rung, where a lost
// datagram is normal and re-asking is harmless, and 0 for a command, which
// may be relative (a volume step) and must not be sent twice unless its
// binding says so.
func TestRetriesDefaultByFamily(t *testing.T) {
	if got := (driver.Request{}).RetriesOr(driver.DefaultPollRetries); got != 2 {
		t.Fatalf("a poll's default retries = %d, want 2", got)
	}
	if got := (driver.Request{}).RetriesOr(driver.DefaultCommandRetries); got != 0 {
		t.Fatalf("a command's default retries = %d, want 0", got)
	}
	if got := (driver.Request{Retries: intp(1)}).RetriesOr(driver.DefaultCommandRetries); got != 1 {
		t.Fatalf("a declared retry = %d, want 1", got)
	}
}

func TestCheckInputValueListenPort(t *testing.T) {
	s := udpSpec()
	if err := s.CheckInputValue(driver.ListenPortInput, "51327"); err != nil {
		t.Fatalf("a good listen port refused: %v", err)
	}
	for _, bad := range []string{"0", "65536", "x"} {
		if err := s.CheckInputValue(driver.ListenPortInput, bad); err == nil {
			t.Errorf("listen port %q accepted", bad)
		}
	}
}

// A rendered command request keeps its binding's retries: the delivery
// reads them off the rendered request.
func TestRenderRequestKeepsRetries(t *testing.T) {
	two := 2
	got, err := driver.RenderRequest(driver.Request{Line: "SET INPUT ${value}", Retries: &two}, "hdmi2", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Line != "SET INPUT hdmi2" || got.RetriesOr(driver.DefaultCommandRetries) != 2 {
		t.Fatalf("rendered %+v, want the line and the declared retries", got)
	}
}
