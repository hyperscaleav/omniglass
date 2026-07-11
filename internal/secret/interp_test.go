package secret

import (
	"fmt"
	"reflect"
	"testing"
)

func TestParseRefs(t *testing.T) {
	got := ParseRefs("host=$var:endpoints.primary.host community=$sec:snmp_ro plain")
	want := []Ref{
		{Kind: "var", Name: "endpoints", Path: []string{"primary", "host"}, Raw: "$var:endpoints.primary.host"},
		{Kind: "sec", Name: "snmp_ro", Path: nil, Raw: "$sec:snmp_ro"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parse refs\n got %+v\nwant %+v", got, want)
	}
}

func TestInterpolate(t *testing.T) {
	out, err := Interpolate("u=$sec:auth.username p=$sec:auth.password", func(r Ref) (string, error) {
		switch {
		case r.Name == "auth" && len(r.Path) == 1 && r.Path[0] == "username":
			return "svc", nil
		case r.Name == "auth" && len(r.Path) == 1 && r.Path[0] == "password":
			return "hunter2", nil
		}
		return "", fmt.Errorf("unresolved %s", r.Raw)
	})
	if err != nil {
		t.Fatalf("interpolate: %v", err)
	}
	if out != "u=svc p=hunter2" {
		t.Fatalf("got %q", out)
	}
}

func TestInterpolateUnresolvedErrors(t *testing.T) {
	_, err := Interpolate("$sec:missing", func(Ref) (string, error) { return "", fmt.Errorf("nope") })
	if err == nil {
		t.Fatalf("expected error on unresolved ref")
	}
}

func TestDotGet(t *testing.T) {
	v := map[string]any{
		"primary": map[string]any{"host": "10.0.0.1", "port": 161},
	}
	got, err := DotGet(v, []string{"primary", "host"})
	if err != nil {
		t.Fatalf("dotget: %v", err)
	}
	if got != "10.0.0.1" {
		t.Fatalf("got %v", got)
	}
	if _, err := DotGet(v, []string{"primary", "nope"}); err == nil {
		t.Fatalf("expected error on missing path")
	}
}

// Masking renders a secret field as a fixed placeholder, never the value, for
// the resolve view and any log line.
func TestMaskField(t *testing.T) {
	shape := Shape{Name: "basic_auth", Fields: []Field{
		{Name: "username", Secret: false, Origin: OriginOperator},
		{Name: "password", Secret: true, Origin: OriginOperator},
	}}
	if MaskField(shape, "username", "svc") != "svc" {
		t.Fatalf("non-secret field should render plaintext")
	}
	if got := MaskField(shape, "password", "hunter2"); got != Masked {
		t.Fatalf("secret field should be masked, got %q", got)
	}
	// An unknown field is masked defensively (fail closed).
	if got := MaskField(shape, "mystery", "x"); got != Masked {
		t.Fatalf("unknown field should mask, got %q", got)
	}
}
