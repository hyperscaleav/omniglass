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
		Ladder:     &driver.EffectiveLadder{Ping: true, Transport: driver.RungConnect, Session: true},
		HostKeyPin: "SHA256:pinned",
		Retry:      1,
	}
}

func TestFingerprintIsStableAndOpaque(t *testing.T) {
	a, b := fingerprint.Of(base()), fingerprint.Of(base())
	if a != b || len(a) != 64 {
		t.Fatalf("fingerprints %q, %q: want one stable sha256 hex", a, b)
	}
	for _, clear := range []string{"10.0.0.5", "panel-login", "sealed-digest-1", "SHA256:pinned", "auth rejected"} {
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
