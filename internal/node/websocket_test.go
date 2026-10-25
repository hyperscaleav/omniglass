package node

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// A websocket endpoint's session as the node derives it (#1034): its auth
// step is the upgrade (the driver's, or the root), riding the session's
// scheme with the credential resolved from the delivered secrets; its polls
// never fall back to a throwaway line connection; and its learned auth is
// kept under its fingerprint like an http endpoint's.

func wsPollTask(id, endpointID string, ss *driver.SessionSpec, password, fingerprint string) collection.TaskSpec {
	return collection.TaskSpec{
		ID: id, Mode: "poll", Transport: "wss", EndpointID: endpointID, EndpointName: "wss",
		EndpointParams: json.RawMessage(`{"target":"10.0.0.7:9443","tls-mode":"pin"}`),
		Spec: json.RawMessage(`{"driver":"kestrel-ws","function":"volume","schedule":{"every":"60s"},
			"request":{"body":{"type":"get","path":"status.audio.volume"},"reply":{"correlate":"id"}},
			"emits":[{"name":"volume","lane":"metric","extract":{"jsonpath":"value"}}]}`),
		Secrets:     map[string]map[string]string{"login": {"username": "admin", "password": password}},
		Session:     ss,
		Fingerprint: fingerprint,
	}
}

func basicUpgrade(path string) *driver.SessionSpec {
	ss := &driver.SessionSpec{Credential: "login", Scheme: driver.SchemeBasic}
	if path != "" {
		ss.Upgrade = &driver.Request{Path: path}
	}
	return ss
}

func TestAWebSocketSessionsAuthStepIsItsUpgrade(t *testing.T) {
	dialers := map[string]collection.SessionDialer{"wss": collection.NewWebSocketDialer()}
	want, refused := desiredSessions([]collection.TaskSpec{wsPollTask("p1", "ep-1", basicUpgrade("/api/ws"), "kestrel", "fp-1")}, dialers)
	if len(refused) != 0 {
		t.Fatalf("refused = %v", refused)
	}
	w, ok := want["ep-1"]
	if !ok || w.auth == nil || w.auth.HTTP == nil {
		t.Fatalf("desired = %+v, want a session whose auth step is the upgrade", want)
	}
	a := w.auth.HTTP
	if a.Request.Path != "/api/ws" || a.Credential == nil || a.Credential.Username != "admin" || a.Credential.Password != "kestrel" {
		t.Fatalf("upgrade auth = %+v, want /api/ws carrying the delivered credential", a.Request)
	}
	if w.tls == nil || w.tls.Mode != driver.TLSModePin {
		t.Fatalf("a wss session's trust = %+v, want the endpoint's pin mode", w.tls)
	}

	// No upgrade declared: the root. No session block: still an upgrade,
	// with no credential.
	want, _ = desiredSessions([]collection.TaskSpec{wsPollTask("p1", "ep-1", basicUpgrade(""), "kestrel", "fp-1")}, dialers)
	if got := want["ep-1"].auth.HTTP.Request.Path; got != "/" {
		t.Fatalf("an undeclared upgrade's path = %q, want the root", got)
	}
	want, _ = desiredSessions([]collection.TaskSpec{wsPollTask("p1", "ep-1", nil, "kestrel", "fp-1")}, dialers)
	if a := want["ep-1"].auth; a == nil || a.HTTP == nil || a.HTTP.Credential != nil {
		t.Fatalf("an open websocket's auth = %+v, want an uncredentialed upgrade", a)
	}

	// A credential the scheme needs and the worklist did not deliver refuses
	// the session by reference, never by value.
	task := wsPollTask("p1", "ep-1", basicUpgrade("/api/ws"), "kestrel", "fp-1")
	task.Secrets = map[string]map[string]string{"login": {"username": "admin"}}
	_, refused = desiredSessions([]collection.TaskSpec{task}, dialers)
	if reason := refused["ep-1"]; !strings.Contains(reason, "login.password") || strings.Contains(reason, "admin") {
		t.Fatalf("refused = %v, want the missing reference named and nothing else", refused)
	}
}

func TestAWebSocketPollNeverFallsBackToAThrowawayConnection(t *testing.T) {
	throwaway := &lineRecorder{}
	runner := &collection.Runner{Line: throwaway}
	// Even an open websocket (no session block) rides only its held socket.
	_, faults := collectDriverTask(t.Context(), runner, wsPollTask("p1", "ep-1", nil, "kestrel", "fp-1"), nil)
	if len(faults) != 1 || !strings.Contains(faults[0].Error(), "no session") {
		t.Fatalf("faults = %v, want the missing session named", faults)
	}
	if len(throwaway.sent) != 0 {
		t.Fatalf("a websocket poll dialed a throwaway line connection: %v", throwaway.sent)
	}
}

func TestAWebSocketEndpointsLearnedAuthIsKeptUnderItsFingerprint(t *testing.T) {
	m := newSessionManager(nil, sessionHooks{})
	task := wsPollTask("p1", "ep-1", basicUpgrade("/api/ws"), "kestrel", "fp-1")
	state := m.httpAuthOf("ep-1", "fp-1")
	m.syncHTTPEndpoints([]collection.TaskSpec{task})
	if m.httpAuthOf("ep-1", "fp-1") != state {
		t.Fatal("a websocket endpoint's learned auth was dropped while its fingerprint held")
	}
	task.Fingerprint = "fp-2"
	m.syncHTTPEndpoints([]collection.TaskSpec{task})
	if m.httpAuthOf("ep-1", "fp-1") == state {
		t.Fatal("a websocket endpoint's learned auth outlived its fingerprint")
	}
	m.syncHTTPEndpoints(nil)
	if _, held := m.httpAuth["ep-1"]; held {
		t.Fatal("a websocket endpoint's learned auth outlived the endpoint")
	}
}
