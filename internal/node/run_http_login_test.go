//go:build linux

package node_test

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// http logins and digest end to end (#1114): Postgres, the bus, a real
// node, and the simulator's login faces on ephemeral ports. A form login
// keeps one session across many polls and logs in exactly once more when the
// panel forgets it; a JSON token login does the same with its captured
// token; digest answers its challenge and a stale nonce. A wrong password is
// one attempt, across ticks and a node restart, its reason on the gate; the
// secret fixed in place brings the endpoint back.

// loginProfile is the simulator profile whose http face enforces a login of
// the auth type, derived from the catalog, never named (#804).
func loginProfile(t *testing.T, store *profile.Store, typ string) string {
	t.Helper()
	var names []string
	for name, d := range store.Devices {
		for _, iface := range d.Interfaces {
			if iface.Protocol == "http" && iface.Auth.Enforce && iface.Auth.Type == typ {
				names = append(names, name)
			}
		}
	}
	if len(names) == 0 {
		t.Fatalf("no simulator profile serves a %s login", typ)
	}
	sort.Strings(names)
	return names[0]
}

// loginVariant adds a copy of a profile under name whose http face logs in
// by auth.
func loginVariant(store *profile.Store, from, name string, auth profile.Auth) {
	base := *store.Devices[from]
	base.Name = name
	base.Interfaces = append([]profile.Interface(nil), base.Interfaces...)
	for i := range base.Interfaces {
		if base.Interfaces[i].Protocol == "http" {
			base.Interfaces[i].Auth = auth
		}
	}
	store.Devices[name] = &base
}

// faceAuth is a profile's http face's auth block.
func faceAuth(store *profile.Store, name string) profile.Auth {
	for _, iface := range store.Devices[name].Interfaces {
		if iface.Protocol == "http" {
			return iface.Auth
		}
	}
	return profile.Auth{}
}

// quickDriver authors a copy of a shipped driver whose polls run every
// 300ms, so a test watches many polls ride one session.
func (s *httpStack) quickDriver(shipped, name string) {
	s.t.Helper()
	d, err := s.gw.GetDriver(s.ctx, shipped)
	if err != nil {
		s.t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(d.Spec, &spec); err != nil {
		s.t.Fatal(err)
	}
	for _, p := range spec["polls"].([]any) {
		p.(map[string]any)["schedule"] = map[string]any{"every": "300ms"}
	}
	s.authorDriver(name, spec)
}

func (s *httpStack) authorDriver(name string, spec map[string]any) {
	s.t.Helper()
	raw, err := json.Marshal(spec)
	if err != nil {
		s.t.Fatal(err)
	}
	if _, err := s.gw.CreateDriver(s.ctx, "", storage.Driver{Name: name, Label: name, Version: "1.0.0", Spec: raw}); err != nil {
		s.t.Fatalf("author %s: %v", name, err)
	}
}

// ungated is an endpoint-rejection no gate holds: never published, or none.
func ungated(v string) bool { return v == "" || v == "none" }

// grows waits for count to pass from by at least n.
func (s *httpStack) grows(what string, count func() int64, from, n int64) {
	s.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for count() < from+n && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if got := count(); got < from+n {
		s.t.Fatalf("%s = %d, want at least %d", what, got, from+n)
	}
}

func (s *httpStack) setPassword(sec *storage.Secret, pass string) {
	s.t.Helper()
	if _, err := s.gw.UpdateSecret(s.ctx, "", sec.ID, storage.SecretPatch{Fields: map[string]string{"username": "admin", "password": pass}}, s.all, s.all, true, true); err != nil {
		s.t.Fatal(err)
	}
}

// refusedOnceThenRecovers sets a wrong password, watches the endpoint read
// rejected with reason after exactly one more attempt (counted by
// attempts), holds that across a node restart, then fixes the secret in
// place and watches it recover.
func (s *httpStack) refusedOnceThenRecovers(nodeName, comp, instance string, sec *storage.Secret, right string, attempts func() int64, perAttempt int64, reason string) {
	s.t.Helper()
	before := attempts()
	s.setPassword(sec, "wrong-password")
	s.waitFor(comp, instance, "endpoint-authenticated", is("no"), "no: the wrong password refused")
	s.waitFor(comp, instance, "endpoint-rejection", func(v string) bool { return strings.HasPrefix(v, "authenticated auth ") }, "the auth gate")
	s.waitFor(comp, instance, "endpoint-rejection-reason", is(reason), "the gate's reason, for the notice")
	s.settles("attempts with the wrong password", attempts, before+perAttempt)
	s.runNode(nodeName)
	s.settles("attempts after a node restart", attempts, before+perAttempt)
	// Fixed in place: the fingerprint moves, the gate lifts.
	s.setPassword(sec, right)
	s.waitFor(comp, instance, "endpoint-rejection", is("none"), "none, the secret fixed")
	s.waitFor(comp, instance, "endpoint-authenticated", is("yes"), "yes after the fix")
}

func TestNodeHTTPFormAndTokenLoginsKeepTheirSession(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	var formProfile string
	tokenProfile := "panel-token"
	s := newHTTPStack(t, "edge-login", func(store *profile.Store) {
		formProfile = loginProfile(t, store, "form")
		base := faceAuth(store, formProfile)
		loginVariant(store, formProfile, tokenProfile, profile.Auth{Type: "token", DefaultUser: base.DefaultUser, DefaultPassword: base.DefaultPassword,
			Enforce: true, SessionTTL: 600})
	}, func(*profile.Store) []string { return []string{formProfile, tokenProfile} })
	panel, tokenPanel := s.devices[formProfile], s.devices[tokenProfile]
	password := fmt.Sprint(panel.HTTPAuth.Password)

	// The form login: the shipped driver, its polls quickened.
	s.quickDriver("newtron-panel-web", "panel-web-quick")
	s.component("panel-1")
	formSecret := s.secret("panel-login", "basic-auth", map[string]string{"username": "admin", "password": password})
	fep := s.attachHTTP("panel-1", "panel-web-quick", "edge-login", map[string]string{
		"host": s.ips[formProfile], "port": fmt.Sprint(s.httpPort(formProfile)), "login": "panel-login"})

	// The token login: a driver authored here, its token captured and sent
	// as a bearer, renewed before its declared expiry.
	s.authorDriver("panel-token-api", map[string]any{
		"version": 1, "transport": "https",
		"inputs": []any{
			map[string]any{"name": "host", "kind": "string", "required": true},
			map[string]any{"name": "port", "kind": "number", "default": "443"},
			map[string]any{"name": "tls-mode", "kind": "string", "default": "pin"},
			map[string]any{"name": "login", "kind": "secret", "secret_type": "basic-auth", "required": true},
		},
		"session": map[string]any{"login": map[string]any{
			"steps": []any{map[string]any{"request": map[string]any{"method": "POST", "path": "/api/login",
				"body": map[string]any{"username": "${secret.login.username}", "password": "${secret.login.password}"}}}},
			"capture": []any{map[string]any{"name": "token", "extract": map[string]any{"jsonpath": "token"}}},
			"expiry":  map[string]any{"jsonpath": "expires_in"},
			"headers": map[string]any{"Authorization": "Bearer ${session.token}"},
			"expired": map[string]any{"regex": "token expired"},
		}},
		"polls": []any{map[string]any{"name": "status", "schedule": map[string]any{"every": "300ms"}, "request": map[string]any{"path": "/api/status"},
			"emits": []any{map[string]any{"name": "serial-number", "extract": map[string]any{"jsonpath": "device.serial"}}}}},
	})
	s.component("panel-2")
	tokenSecret := s.secret("panel-token-login", "basic-auth", map[string]string{"username": "admin", "password": password})
	tep := s.attachHTTP("panel-2", "panel-token-api", "edge-login", map[string]string{
		"host": s.ips[tokenProfile], "port": fmt.Sprint(s.httpPort(tokenProfile)), "login": "panel-token-login"})
	s.runNode("edge-login")

	s.waitFor("panel-1", fep.Name, "endpoint-authenticated", is("yes"), "yes: the form login opened a session")
	s.waitFor("panel-1", fep.Name, "model-number", is(fmt.Sprint(panel.ReadPath("configuration.device.model"))), "the model, read inside the session")
	s.waitFor("panel-2", tep.Name, "endpoint-authenticated", is("yes"), "yes: the token login")
	s.waitFor("panel-2", tep.Name, "serial-number", is(fmt.Sprint(tokenPanel.ReadPath("configuration.device.serial"))), "the serial, read with the token")

	for _, c := range []struct {
		name string
		dev  interface {
			HTTPRequests() int64
			HTTPLogins() int64
			ExpireHTTPSessions() int
		}
	}{{"form", panel}, {"token", tokenPanel}} {
		// Many polls, one login.
		s.grows(c.name+" requests", c.dev.HTTPRequests, c.dev.HTTPRequests(), 8)
		if got := c.dev.HTTPLogins(); got != 1 {
			t.Fatalf("%s: %d logins across many polls, want the one session kept", c.name, got)
		}
		// The component forgets every session: exactly one more login.
		c.dev.ExpireHTTPSessions()
		s.settles(c.name+" logins after the expiry", c.dev.HTTPLogins, 2)
		s.grows(c.name+" requests after the re-login", c.dev.HTTPRequests, c.dev.HTTPRequests(), 4)
		if got := c.dev.HTTPLogins(); got != 2 {
			t.Fatalf("%s: %d logins after the expiry, want exactly one more", c.name, got)
		}
	}
	s.waitFor("panel-1", fep.Name, "endpoint-rejection", ungated, "no gate: an expired session is not a refusal")

	// A wrong password: one login attempt, never a loop.
	s.refusedOnceThenRecovers("edge-login", "panel-1", fep.Name, formSecret, password, panel.HTTPLogins, 1,
		"auth: the login was sent back to the login page (302 to /login)")
	s.refusedOnceThenRecovers("edge-login", "panel-2", tep.Name, tokenSecret, password, tokenPanel.HTTPLogins, 1,
		`auth: the component refused the login (401): {"error":"invalid credentials"}`)
}

func TestNodeHTTPDigestAnswersItsChallenge(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	var md5Profile string
	shaProfile := "display-sha256"
	s := newHTTPStack(t, "edge-digest", func(store *profile.Store) {
		md5Profile = loginProfile(t, store, "digest")
		a := faceAuth(store, md5Profile)
		a.Algorithm = "SHA-256"
		loginVariant(store, md5Profile, shaProfile, a)
	}, func(*profile.Store) []string { return []string{md5Profile, shaProfile} })
	s.quickDriver("boreal-edge-web", "display-web-quick")
	owner := s.owner()

	for i, name := range []string{md5Profile, shaProfile} {
		dev := s.devices[name]
		password := dev.HTTPAuth.Password
		comp, secret := fmt.Sprintf("display-%d", i+1), fmt.Sprintf("display-login-%d", i+1)
		s.component(comp)
		sec := s.secret(secret, "basic-auth", map[string]string{"username": "admin", "password": password})
		ep := s.attachHTTP(comp, "display-web-quick", "edge-digest", map[string]string{
			"host": s.ips[name], "port": fmt.Sprint(s.httpPort(name)), "login": secret})
		s.runNode("edge-digest")

		s.waitFor(comp, ep.Name, "endpoint-authenticated", is("yes"), name+": yes, the digest answered")
		s.waitFor(comp, ep.Name, "serial-number", is(fmt.Sprint(dev.ReadPath("configuration.device.serial"))), name+": the serial")
		// The challenge is held: each poll answers it at once.
		answers := dev.HTTPDigestAnswers
		s.grows(name+" answered requests", answers, answers(), 6)

		// The display rotates its nonce: stale, answered afresh, never a
		// refusal.
		dev.ExpireHTTPSessions()
		s.grows(name+" answers after the nonce rotated", answers, answers(), 4)
		s.waitFor(comp, ep.Name, "endpoint-rejection", ungated, name+": a stale nonce is not a refusal")

		// A command shares the held challenge.
		if _, err := s.gw.IssueCommand(s.ctx, owner, "component", comp, "set-power", "", json.RawMessage(`"standby"`), nil); err != nil {
			t.Fatalf("issue set-power: %v", err)
		}
		deadline := time.Now().Add(20 * time.Second)
		for fmt.Sprint(dev.ReadPath("status.device.power")) != "standby" && time.Now().Before(deadline) {
			time.Sleep(25 * time.Millisecond)
		}
		if got := fmt.Sprint(dev.ReadPath("status.device.power")); got != "standby" {
			t.Fatalf("%s: power = %s after set-power, want standby", name, got)
		}

		// A wrong password: one answered attempt (the bare request the
		// fresh state sends first draws the challenge it answers).
		s.refusedOnceThenRecovers("edge-digest", comp, ep.Name, sec, password, answers, 1, "auth: the component refused the credential (401)")
	}
}
