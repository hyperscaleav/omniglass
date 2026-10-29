//go:build linux

package node_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A server-sent events stream end to end (#1038): Postgres, the bus, a real
// node, and the simulated Kestrel bar's event stream over https. A change
// on the bar lands within a second with no poll; a dropped stream reads
// recovering and resumes from its last event id, landing every change once;
// a refused credential reads rejected with its reason and is not retried
// under the fingerprint, across a restart; the secret fixed in place brings
// the stream back; and a stream behind a form login logs in first.

// streamDriver authors a copy of a shipped driver with its polls removed but
// the first, so a key a listener lands comes from the stream alone.
func (s *httpStack) streamDriver(shipped, name string) {
	s.t.Helper()
	d, err := s.gw.GetDriver(s.ctx, shipped)
	if err != nil {
		s.t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(d.Spec, &spec); err != nil {
		s.t.Fatal(err)
	}
	spec["polls"] = spec["polls"].([]any)[:1]
	s.authorDriver(name, spec)
}

// volumes is every volume sample the endpoint landed, oldest first.
func (s *httpStack) volumes(comp, instance string) []float64 {
	s.t.Helper()
	c, err := s.gw.GetComponent(s.ctx, comp, s.all)
	if err != nil {
		s.t.Fatal(err)
	}
	rows, err := s.gw.ListMetricSeries(s.ctx, "component", c.ID, "volume", time.Hour, 500)
	if err != nil {
		s.t.Fatal(err)
	}
	var out []float64
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Instance == instance {
			out = append(out, rows[i].Value)
		}
	}
	return out
}

// sessionStates is the endpoint's session-state history, oldest first.
func (s *httpStack) sessionStates(comp, instance string) []string {
	s.t.Helper()
	c, err := s.gw.GetComponent(s.ctx, comp, s.all)
	if err != nil {
		s.t.Fatal(err)
	}
	rows, err := s.gw.ListPropertySeries(s.ctx, "component", c.ID, "session-state", time.Hour, 500)
	if err != nil {
		s.t.Fatal(err)
	}
	var out []string
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Instance == instance {
			out = append(out, rows[i].Value)
		}
	}
	return out
}

// setVolume changes the bar's volume the way its web admin would.
func setVolume(t *testing.T, bar *sim.Device, v int) {
	t.Helper()
	if res := bar.Operate(sim.Operation{Kind: "http", Method: "POST", Path: "/api/audio/volume", Body: fmt.Sprintf(`{"volume": %d}`, v)}); res.Status != 200 {
		t.Fatalf("set volume %d = %+v", v, res)
	}
}

func (s *httpStack) waitVolume(comp, instance string, want float64) time.Duration {
	s.t.Helper()
	start := time.Now()
	deadline := start.Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if v, ok := s.metric(comp, instance, "volume"); ok && v == want {
			return time.Since(start)
		}
		time.Sleep(10 * time.Millisecond)
	}
	v, _ := s.metric(comp, instance, "volume")
	s.t.Fatalf("volume on %s[%s] = %v, want %v", comp, instance, v, want)
	return 0
}

func TestNodeHTTPStreamLandsChangesAndResumes(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	var barProfile string
	s := newHTTPStack(t, "edge-stream", nil, func(store *profile.Store) []string {
		barProfile = driverProfile(t, store, "kestrel-api", "websocket") // the bar (only it has the control socket)
		return []string{barProfile}
	})
	bar := s.devices[barProfile]
	s.streamDriver("kestrel-api", "bar-stream")
	s.component("bar-1")
	login := s.secret("bar-login", "basic-auth", map[string]string{"username": "admin", "password": "kestrel"})
	ep := s.attachHTTP("bar-1", "bar-stream", "edge-stream", map[string]string{
		"host": s.ips[barProfile], "port": fmt.Sprint(s.httpPort(barProfile)), "login": "bar-login"})
	s.runNode("edge-stream")

	s.waitFor("bar-1", ep.Name, "session-state", is("up"), "up: the stream held")
	s.waitFor("bar-1", ep.Name, "endpoint-authenticated", is("yes"), "yes: the stream's credential accepted")

	// A change on the bar lands within a second, with no poll to read it.
	setVolume(t, bar, 37)
	if took := s.waitVolume("bar-1", ep.Name, 37); took > time.Second {
		t.Fatalf("the change landed %v after it happened, want within a second", took)
	}

	// The bar drops the stream and changes twice while the node waits the
	// retry it asked for: the stream reads recovering, resumes from its last
	// event id, and lands each change once (the bar's replay sends the last
	// event again, which the node drops).
	if n := bar.DropEventStreams(); n != 1 {
		t.Fatalf("dropped %d streams, want the node's one", n)
	}
	setVolume(t, bar, 38)
	setVolume(t, bar, 39)
	s.waitVolume("bar-1", ep.Name, 39)
	setVolume(t, bar, 40)
	s.waitVolume("bar-1", ep.Name, 40)
	time.Sleep(300 * time.Millisecond)
	if got := s.volumes("bar-1", ep.Name); !slices.Equal(got, []float64{37, 38, 39, 40}) {
		t.Fatalf("volume samples = %v, want each change exactly once", got)
	}
	if states := s.sessionStates("bar-1", ep.Name); !slices.Contains(states, "recovering") || states[len(states)-1] != "up" {
		t.Fatalf("session states = %v, want a recovery back to up", states)
	}
	if bar.EventStreams() != 2 {
		t.Fatalf("streams opened = %d, want the first and its one resume", bar.EventStreams())
	}

	// A wrong password: the fingerprint moves, the stream reconnects once,
	// is refused, and reads rejected with the reason; it does not try again
	// under the fingerprint, across a node restart.
	attempts := bar.EventStreamAttempts
	before := attempts()
	refusedBefore := bar.HTTPCredentialRefusals()
	if _, err := s.gw.UpdateSecret(s.ctx, "", login.ID, storage.SecretPatch{Fields: map[string]string{"password": "wrong-password"}}, s.all, s.all, true, true); err != nil {
		t.Fatal(err)
	}
	s.waitFor("bar-1", ep.Name, "session-state", is("rejected"), "rejected: the stream's credential refused")
	s.waitFor("bar-1", ep.Name, "session-reason", is("auth: the component refused the credential (401)"), "the refusal's reason")
	s.waitFor("bar-1", ep.Name, "endpoint-rejection", func(v string) bool { return strings.HasPrefix(v, "authenticated auth ") }, "the auth gate")
	s.settles("stream attempts with the wrong password", attempts, before+1)
	// The poll waited for the stream's verdict: the wrong password was sent
	// once under the fingerprint, by the stream, and by nothing beside it.
	if got := bar.HTTPCredentialRefusals() - refusedBefore; got != 1 {
		t.Fatalf("credentialed requests refused = %d, want the stream's one", got)
	}
	s.runNode("edge-stream")
	s.settles("stream attempts after a node restart", attempts, before+1)

	// Fixed in place: the gate lifts and the stream comes back up.
	if _, err := s.gw.UpdateSecret(s.ctx, "", login.ID, storage.SecretPatch{Fields: map[string]string{"password": "kestrel"}}, s.all, s.all, true, true); err != nil {
		t.Fatal(err)
	}
	s.waitFor("bar-1", ep.Name, "session-state", is("up"), "up after the fix")
	setVolume(t, bar, 41)
	s.waitVolume("bar-1", ep.Name, 41)
}

func TestNodeHTTPStreamBehindAFormLogin(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	formBar := "bar-form"
	s := newHTTPStack(t, "edge-stream-login", func(store *profile.Store) {
		base := driverProfile(t, store, "kestrel-api", "websocket") // the bar (only it has the control socket)
		loginVariant(store, base, formBar, profile.Auth{Type: "form", DefaultUser: "admin", DefaultPassword: "kestrel", Enforce: true})
	}, func(*profile.Store) []string { return []string{formBar} })
	bar := s.devices[formBar]
	s.authorDriver("bar-form-stream", map[string]any{
		"version": 1, "transport": "https",
		"inputs": []any{
			map[string]any{"name": "host", "kind": "string", "required": true},
			map[string]any{"name": "port", "kind": "number", "default": "443"},
			map[string]any{"name": "tls-mode", "kind": "string", "default": "pin"},
			map[string]any{"name": "login", "kind": "secret", "secret_type": "basic-auth", "required": true},
		},
		"session": map[string]any{
			"login": map[string]any{"path": "/login", "steps": []any{map[string]any{"request": map[string]any{
				"method": "POST", "path": "/login", "body_format": "form",
				"body": map[string]any{"username": "${secret.login.username}", "password": "${secret.login.password}"}}}}},
			"stream": map[string]any{"path": "/api/events"},
		},
		"listeners": []any{map[string]any{"name": "volume-change",
			"match": map[string]any{"event": "change", "field": map[string]any{"path": "path", "equals": "status.audio.volume"}},
			"emits": []any{map[string]any{"name": "volume", "extract": map[string]any{"jsonpath": "value"}}}}},
	})
	s.component("bar-2")
	s.secret("bar-form-login", "basic-auth", map[string]string{"username": "admin", "password": "kestrel"})
	ep := s.attachHTTP("bar-2", "bar-form-stream", "edge-stream-login", map[string]string{
		"host": s.ips[formBar], "port": fmt.Sprint(s.httpPort(formBar)), "login": "bar-form-login"})
	s.runNode("edge-stream-login")

	s.waitFor("bar-2", ep.Name, "session-state", is("up"), "up: logged in, then the stream held")
	s.waitFor("bar-2", ep.Name, "endpoint-authenticated", is("yes"), "yes: the login accepted")
	setVolume(t, bar, 22)
	if took := s.waitVolume("bar-2", ep.Name, 22); took > time.Second {
		t.Fatalf("the change landed %v after it happened", took)
	}
	if bar.HTTPLogins() != 1 {
		t.Fatalf("logins = %d, want the one the stream opened with", bar.HTTPLogins())
	}
	// The bar forgets its sessions and drops the stream: the reconnect is
	// sent to the login page, logs in once more, and resumes.
	bar.ExpireHTTPSessions()
	bar.DropEventStreams()
	setVolume(t, bar, 23)
	s.waitVolume("bar-2", ep.Name, 23)
	if bar.HTTPLogins() != 2 {
		t.Fatalf("logins after the lapse = %d, want exactly one more", bar.HTTPLogins())
	}
}
