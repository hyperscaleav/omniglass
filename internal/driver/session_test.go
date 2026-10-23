package driver_test

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The session block (#1049): a driver declares the auth step a held session
// runs before anything rides it (a login dialog), the replies that are
// terminal (each with a class), and an optional slow re-probe.

func sessionSpec() *driver.Spec {
	s := validSpec()
	s.Session = &driver.SessionSpec{
		Login: &driver.Login{
			Steps: []driver.LoginStep{
				{Expect: driver.Match{Regex: `login: ?$`}, Send: "${secret.login.username}"},
				{Expect: driver.Match{Regex: `Password: ?$`}, Send: "${secret.login.password}"},
			},
			Ready: driver.Match{Prefix: "READY"},
		},
		Terminal: []driver.TerminalRule{
			{Class: driver.ClassAuth, Match: driver.Match{Prefix: "auth rejected"}},
			{Class: driver.ClassAuth, Match: driver.Match{Regex: `(?i)account locked`}},
		},
		Reprobe: "1h",
	}
	return s
}

func TestValidateAcceptsASessionBlock(t *testing.T) {
	if err := sessionSpec().Validate(catalog()); err != nil {
		t.Fatalf("valid session block refused: %v", err)
	}
}

func TestValidateSessionRefusals(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*driver.Spec)
		want   string
	}{
		{"a session on a transport with no held line", func(s *driver.Spec) {
			s.Transport, s.Polls, s.Listeners, s.Commands = "snmp", nil, nil, nil
			s.Polls = []driver.Poll{{Name: "p", Schedule: driver.Schedule{Every: "1m"}, Request: driver.Request{Get: []string{"1.3"}},
				Emits: []driver.Emit{{Name: "uptime", Extract: driver.Extract{OID: "1.3"}}}}}
		}, "snmp"},
		{"a login with no steps", func(s *driver.Spec) { s.Session.Login.Steps = nil }, "step"},
		{"a login step with no prompt", func(s *driver.Spec) { s.Session.Login.Steps[0].Expect = driver.Match{} }, "expect"},
		{"a login step with nothing to send", func(s *driver.Spec) { s.Session.Login.Steps[1].Send = "" }, "send"},
		{"a login with no ready match", func(s *driver.Spec) { s.Session.Login.Ready = driver.Match{} }, "ready"},
		{"a login send naming an undeclared input", func(s *driver.Spec) {
			s.Session.Login.Steps[0].Send = "${secret.cred.username}"
		}, "cred"},
		{"a login send naming a non-secret input", func(s *driver.Spec) {
			s.Session.Login.Steps[0].Send = "${secret.host.username}"
		}, "host"},
		{"a login send with an unknown template field", func(s *driver.Spec) {
			s.Session.Login.Steps[0].Send = "${value}"
		}, "value"},
		{"a login send with no field", func(s *driver.Spec) {
			s.Session.Login.Steps[0].Send = "${secret.login}"
		}, "secret.login"},
		{"a terminal rule with no class", func(s *driver.Spec) { s.Session.Terminal[0].Class = "" }, "class"},
		{"a terminal rule with an unknown class", func(s *driver.Spec) { s.Session.Terminal[0].Class = "bad-vibes" }, "bad-vibes"},
		{"a terminal rule with no match", func(s *driver.Spec) { s.Session.Terminal[0].Match = driver.Match{} }, "terminal"},
		{"an unparseable re-probe", func(s *driver.Spec) { s.Session.Reprobe = "sometimes" }, "reprobe"},
		{"a re-probe too fast to be slow", func(s *driver.Spec) { s.Session.Reprobe = "30s" }, "reprobe"},
		{"an ssh credential off ssh", func(s *driver.Spec) { s.Session.Credential = "login" }, "credential"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := sessionSpec()
			tc.mutate(s)
			err := s.Validate(catalog())
			if err == nil {
				t.Fatalf("mutated session validated; want a refusal naming %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refusal %q does not name %q", err, tc.want)
			}
		})
	}
}

// Polls, commands and listeners ride every held line transport, not only tcp.
func TestValidateAcceptsTheLineFamiliesOnEveryLineTransport(t *testing.T) {
	for _, tr := range []string{"tcp", "telnet"} {
		s := validSpec()
		s.Transport = tr
		if err := s.Validate(catalog()); err != nil {
			t.Errorf("a line spec on %s refused: %v", tr, err)
		}
	}
}

func TestRenderLoginSubstitutesSecretFields(t *testing.T) {
	l := sessionSpec().Session.Login
	got, err := l.Render(map[string]map[string]string{"login": {"username": "admin", "password": "s3cret"}})
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != "admin" || got[1] != "s3cret" {
		t.Fatalf("rendered sends = %q", got)
	}
}

func TestRenderLoginRefusesAMissingSecretWithoutNamingItsValue(t *testing.T) {
	l := sessionSpec().Session.Login
	_, err := l.Render(map[string]map[string]string{"login": {"username": "admin"}})
	if err == nil || !strings.Contains(err.Error(), "login.password") {
		t.Fatalf("a missing password rendered: %v", err)
	}
	if strings.Contains(err.Error(), "admin") {
		t.Fatalf("the refusal leaks a credential value: %v", err)
	}
}

// A credential carrying a line terminator would inject a second line into the
// login conversation, so it refuses, and the refusal never echoes the value.
func TestRenderLoginRefusesAControlCharacter(t *testing.T) {
	l := sessionSpec().Session.Login
	_, err := l.Render(map[string]map[string]string{"login": {"username": "admin", "password": "pw\r\nreboot"}})
	if err == nil || !strings.Contains(err.Error(), "control") {
		t.Fatalf("a password carrying CRLF rendered: %v", err)
	}
	if strings.Contains(err.Error(), "pw") || strings.Contains(err.Error(), "reboot") {
		t.Fatalf("the refusal leaks a credential value: %v", err)
	}
}

func TestTerminalClassifiesTheFirstRuleThatMatches(t *testing.T) {
	ss := sessionSpec().Session
	if class, ok := ss.TerminalFor("auth rejected"); !ok || class != driver.ClassAuth {
		t.Fatalf("auth rejected = %q, %v", class, ok)
	}
	if class, ok := ss.TerminalFor("ERR Account Locked for 30m"); !ok || class != driver.ClassAuth {
		t.Fatalf("account locked = %q, %v", class, ok)
	}
	if _, ok := ss.TerminalFor("INPUT hdmi1"); ok {
		t.Fatal("an ordinary answer classified terminal")
	}
	var none *driver.SessionSpec
	if _, ok := none.TerminalFor("auth rejected"); ok {
		t.Fatal("a driver with no session block classified a line terminal")
	}
}

func TestReprobeIntervalParses(t *testing.T) {
	ss := sessionSpec().Session
	if got := ss.ReprobeInterval(); got.String() != "1h0m0s" {
		t.Fatalf("ReprobeInterval = %s", got)
	}
	ss.Reprobe = ""
	if got := ss.ReprobeInterval(); got != 0 {
		t.Fatalf("no re-probe = %s, want 0 (never)", got)
	}
}
