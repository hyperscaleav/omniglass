package driver_test

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The reachability ladder a driver declares (#1056 ruling, ADR-0157): whether
// to ping (informational only, never a gate), what the transport rung is (a
// connect on the transport's port, the protocol's handshake, or a named
// request answered), and whether a session rung exists. Defaults come from
// the transport registry, so most drivers declare nothing.

func TestLadderDefaultsComeFromTheTransport(t *testing.T) {
	cases := []struct {
		transport string
		rung      string
		session   bool
	}{
		{"tcp", driver.RungConnect, false},
		{"telnet", driver.RungConnect, false},
		{"ssh", driver.RungHandshake, true},
		{"http", driver.RungConnect, false},
		{"snmp", driver.RungRequest, false},
		{"udp", driver.RungRequest, false},
	}
	for _, tc := range cases {
		l := driver.ResolveLadder(tc.transport, nil, nil)
		if l.Transport != tc.rung || l.Session != tc.session || !l.Ping {
			t.Errorf("%s: resolved %+v, want rung %s session %v, ping on", tc.transport, l, tc.rung, tc.session)
		}
	}
	// A declared login is a session rung, wherever it rides.
	if l := driver.ResolveLadder("telnet", nil, &driver.SessionSpec{Login: &driver.Login{}}); !l.Session {
		t.Fatal("a telnet login resolved no session rung")
	}
}

// The architect's example: a cloud API is never pinged, but its 443 is
// checked open, then its session runs.
func TestLadderCloudAPINeverPings(t *testing.T) {
	no := false
	l := driver.ResolveLadder("http", &driver.Ladder{Ping: &driver.PingRung{Enabled: &no}}, nil)
	if l.Ping || l.Transport != driver.RungConnect {
		t.Fatalf("resolved %+v, want no ping and a connect", l)
	}
}

func TestValidateLadderRefusesARungItsTransportCannotRun(t *testing.T) {
	cases := []struct {
		transport string
		ladder    driver.Ladder
		want      string
	}{
		{"snmp", driver.Ladder{Transport: &driver.TransportRung{Rung: driver.RungConnect}}, "connect"},
		{"telnet", driver.Ladder{Transport: &driver.TransportRung{Rung: driver.RungHandshake}}, "handshake"},
		{"tcp", driver.Ladder{Transport: &driver.TransportRung{Rung: driver.RungRequest}}, "request"}, // a request rung names its request
		{"tcp", driver.Ladder{Transport: &driver.TransportRung{Rung: "teleport"}}, "teleport"},
	}
	for _, tc := range cases {
		s := validSpec()
		s.Transport = tc.transport
		if tc.transport == "snmp" {
			s.Polls[0].Request, s.Polls[0].Emits[0].Extract = driver.Request{Get: []string{"1.3"}}, driver.Extract{OID: "1.3"}
			s.Listeners, s.Commands = nil, nil
		}
		l := tc.ladder
		s.Ladder = &l
		err := s.Validate(catalog())
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s ladder %+v = %v, want a refusal naming %q", tc.transport, tc.ladder, err, tc.want)
		}
	}
	ok := validSpec()
	ok.Ladder = &driver.Ladder{Transport: &driver.TransportRung{Rung: driver.RungRequest, Request: &driver.Request{Line: "GET INPUT", Reply: &driver.Reply{Prefix: "INPUT "}}}}
	if err := ok.Validate(catalog()); err != nil {
		t.Fatalf("a tcp request rung refused: %v", err)
	}
	session := false
	ok.Ladder = &driver.Ladder{Session: &driver.SessionRung{Enabled: &session}}
	ok.Session = &driver.SessionSpec{Login: &driver.Login{Steps: []driver.LoginStep{{Expect: driver.Match{Prefix: "login:"}, Send: "${secret.login.username}"}}, Ready: driver.Match{Prefix: "R"}}}
	if err := ok.Validate(catalog()); err == nil || !strings.Contains(err.Error(), "session") {
		t.Fatalf("a login with the session rung declared absent = %v, want refused", err)
	}
}
