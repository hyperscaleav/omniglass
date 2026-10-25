package fingerprint_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/fingerprint"
)

// The connection fingerprint (#1056 ruling): a deterministic digest over
// every input a connection to an endpoint is made with. Every field of Inputs
// moves it (a field added later cannot be forgotten), it is stable, and it
// carries no input in the clear.

func base() fingerprint.Inputs {
	return fingerprint.Inputs{
		Transport:      "telnet",
		Params:         json.RawMessage(`{"target":"10.0.0.5:23","timeout":"5s"}`),
		DriverInputs:   json.RawMessage(`{"host":"10.0.0.5","login":"panel-login","port":"23"}`),
		SecretVersions: map[string]string{"login": "sealed-digest-1"},
		Session: &driver.SessionSpec{
			Login:    &driver.Login{Steps: []driver.LoginStep{{Expect: driver.Match{Prefix: "login:"}, Send: "${secret.login.username}"}}, Ready: driver.Match{Prefix: "READY"}},
			Terminal: []driver.TerminalRule{{Class: driver.ClassAuth, Match: driver.Match{Prefix: "auth rejected"}}},
		},
		Ladder:         &driver.EffectiveLadder{Ping: true, Transport: driver.RungConnect, Session: true},
		HostKeyPin:     "SHA256:pinned",
		CertificatePin: "SHA256:tls-pinned",
		Retry:          1,
	}
}

func TestFingerprintIsStableAndOpaque(t *testing.T) {
	a, b := fingerprint.Of(base()), fingerprint.Of(base())
	if a != b || len(a) != 64 {
		t.Fatalf("fingerprints %q, %q: want one stable sha256 hex", a, b)
	}
	for _, clear := range []string{"10.0.0.5", "panel-login", "sealed-digest-1", "SHA256:pinned", "SHA256:tls-pinned", "auth rejected"} {
		if strings.Contains(a, clear) {
			t.Fatalf("the fingerprint carries %q in the clear", clear)
		}
	}
	// Equivalent JSON spellings of the same params are one fingerprint.
	in := base()
	in.Params = json.RawMessage(`{ "timeout": "5s", "target": "10.0.0.5:23" }`)
	if fingerprint.Of(in) != a {
		t.Fatal("a reformatted params document moved the fingerprint")
	}
}

// Every field of Inputs moves the fingerprint: a reflection walk sets each to
// a different value. A field the hash skipped (a json "-" tag, say) fails.
func TestEveryInputMovesTheFingerprint(t *testing.T) {
	ref := fingerprint.Of(base())
	typ := reflect.TypeOf(fingerprint.Inputs{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		in := base()
		v := reflect.ValueOf(&in).Elem().Field(i)
		switch v.Interface().(type) {
		case string:
			v.SetString(v.String() + "-changed")
		case int64:
			v.SetInt(v.Int() + 1)
		case time.Duration:
			v.SetInt(v.Int() + 1)
		case json.RawMessage:
			v.Set(reflect.ValueOf(json.RawMessage(`{"changed":true}`)))
		case map[string]string:
			v.Set(reflect.ValueOf(map[string]string{"login": "sealed-digest-2"}))
		case *driver.EffectiveLadder:
			v.Set(reflect.ValueOf(&driver.EffectiveLadder{Transport: driver.RungRequest}))
		case *driver.SessionSpec:
			v.Set(reflect.ValueOf(&driver.SessionSpec{Reprobe: "1h"}))
		default:
			t.Fatalf("field %s has type %s, which this test cannot vary: teach it", f.Name, f.Type)
		}
		if fingerprint.Of(in) == ref {
			t.Errorf("changing %s left the fingerprint unchanged", f.Name)
		}
	}
}

// Every input an http login is made with moves the fingerprint (#1114): each
// field of the login declaration, the step requests among them, so editing
// how a driver logs in lifts a gate a refusal set, as rotating its secret
// does. A field the session's hash skipped fails.
func TestEveryHTTPLoginInputMovesTheFingerprint(t *testing.T) {
	login := func() *driver.Login {
		return &driver.Login{
			Steps:   []driver.LoginStep{{Request: &driver.Request{Method: "POST", Path: "/login", Body: json.RawMessage(`{"u":"${secret.login.username}"}`)}}},
			Path:    "/login",
			Capture: []driver.Capture{{Name: "token", Extract: driver.Extract{JSONPath: "token"}}},
			Headers: map[string]string{"Authorization": "Bearer ${session.token}"},
			Expired: &driver.Match{Regex: "expired"},
			Expiry:  &driver.Extract{JSONPath: "expires_in"},
		}
	}
	with := func(l *driver.Login) string {
		in := base()
		in.Transport = "http"
		in.Session = &driver.SessionSpec{Login: l}
		return fingerprint.Of(in)
	}
	ref := with(login())
	typ := reflect.TypeOf(driver.Login{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		l := login()
		v := reflect.ValueOf(l).Elem().Field(i)
		switch v.Interface().(type) {
		case string:
			v.SetString(v.String() + "-changed")
		case []driver.LoginStep:
			v.Set(reflect.ValueOf([]driver.LoginStep{{Request: &driver.Request{Method: "POST", Path: "/signin"}}}))
		case driver.Match:
			v.Set(reflect.ValueOf(driver.Match{Prefix: "welcome"}))
		case *driver.Match:
			v.Set(reflect.ValueOf(&driver.Match{Regex: "lapsed"}))
		case []driver.Capture:
			v.Set(reflect.ValueOf([]driver.Capture{{Name: "sid", Extract: driver.Extract{JSONPath: "sid"}}}))
		case map[string]string:
			v.Set(reflect.ValueOf(map[string]string{"X-Token": "${session.token}"}))
		case *driver.Extract:
			v.Set(reflect.ValueOf(&driver.Extract{JSONPath: "ttl"}))
		default:
			t.Fatalf("login field %s has type %s, which this test cannot vary: teach it", f.Name, f.Type)
		}
		if with(l) == ref {
			t.Errorf("changing the login's %s left the fingerprint unchanged", f.Name)
		}
	}
}

// Every input a websocket upgrade is made with moves the fingerprint
// (#1034): its path, its query and its headers, so editing where or how a
// driver upgrades lifts a gate a refusal set, as rotating its secret does.
func TestEveryWebSocketUpgradeInputMovesTheFingerprint(t *testing.T) {
	upgrade := func() *driver.Request {
		return &driver.Request{Path: "/api/ws", Query: map[string]string{"room": "huddle"}, Headers: map[string]string{"Origin": "https://bar"}}
	}
	with := func(u *driver.Request) string {
		in := base()
		in.Transport = "wss"
		in.Session = &driver.SessionSpec{Credential: "login", Scheme: driver.SchemeBasic, Upgrade: u}
		return fingerprint.Of(in)
	}
	ref := with(upgrade())
	for name, edit := range map[string]func(*driver.Request){
		"path":    func(u *driver.Request) { u.Path = "/ws" },
		"query":   func(u *driver.Request) { u.Query = map[string]string{"room": "boardroom"} },
		"headers": func(u *driver.Request) { u.Headers = map[string]string{"Origin": "https://other"} },
	} {
		u := upgrade()
		edit(u)
		if with(u) == ref {
			t.Errorf("changing the upgrade's %s left the fingerprint unchanged", name)
		}
	}
	if with(nil) == ref {
		t.Error("declaring an upgrade left the fingerprint unchanged")
	}
}
