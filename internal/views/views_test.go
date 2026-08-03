package views_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/views"
)

// minimalDef returns a valid definition to mutate per case: one string column,
// a field-mapping onto it, a declared permission, and a scope resource.
func minimalDef(name string) views.Definition {
	return views.Definition{
		Name:          name,
		Summary:       "a test view",
		Permission:    []string{"component", "read"},
		ScopeResource: "component",
		Columns:       []views.Column{{Name: "state", Type: "string", Role: "value"}},
		FieldMapping:  map[string]string{"value": "state"},
		Run: func(context.Context, storage.Gateway, scope.Set, map[string]any, string) ([][]any, string, error) {
			return nil, "", nil
		},
	}
}

// TestBindParamsTypes proves each declared type binds its Go value: string
// passes through, int parses, duration parses Go syntax, time parses RFC3339.
func TestBindParamsTypes(t *testing.T) {
	decl := []views.Param{
		{Name: "owner", Type: "string", Required: true},
		{Name: "limit", Type: "int"},
		{Name: "window", Type: "duration"},
		{Name: "since", Type: "time"},
	}
	got, err := views.BindParams(decl, []string{
		"owner=disp-1", "limit=50", "window=24h", "since=2026-08-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("BindParams: %v", err)
	}
	if got["owner"] != "disp-1" {
		t.Errorf("owner = %v, want disp-1", got["owner"])
	}
	if got["limit"] != 50 {
		t.Errorf("limit = %v (%T), want int 50", got["limit"], got["limit"])
	}
	if got["window"] != 24*time.Hour {
		t.Errorf("window = %v, want 24h", got["window"])
	}
	want := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if ts, ok := got["since"].(time.Time); !ok || !ts.Equal(want) {
		t.Errorf("since = %v, want %v", got["since"], want)
	}
}

// TestBindParamsRejections proves every malformed binding is an error NAMING
// the parameter, so the route's 400 can say which param failed: an undeclared
// name, a missing required one, a pair without '=', an unparseable value, and
// a duplicated name.
func TestBindParamsRejections(t *testing.T) {
	decl := []views.Param{
		{Name: "owner", Type: "string", Required: true},
		{Name: "limit", Type: "int"},
	}
	cases := []struct {
		label string
		pairs []string
		names string // the param the error must name
	}{
		{"undeclared", []string{"owner=a", "bogus=1"}, "bogus"},
		{"missing required", []string{"limit=3"}, "owner"},
		{"malformed pair", []string{"owner"}, "owner"},
		{"bad value", []string{"owner=a", "limit=abc"}, "limit"},
		{"duplicate", []string{"owner=a", "owner=b"}, "owner"},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			_, err := views.BindParams(decl, c.pairs)
			if err == nil {
				t.Fatalf("BindParams(%v): want an error, got none", c.pairs)
			}
			if !strings.Contains(err.Error(), c.names) {
				t.Errorf("error %q does not name %q", err.Error(), c.names)
			}
		})
	}
}

// TestBindParamsEmpty proves a view with no declared params binds an empty map
// from no pairs, and rejects any pair at all as undeclared.
func TestBindParamsEmpty(t *testing.T) {
	got, err := views.BindParams(nil, nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("BindParams(nil, nil) = %v, %v, want empty map", got, err)
	}
	if _, err := views.BindParams(nil, []string{"x=1"}); err == nil || !strings.Contains(err.Error(), "x") {
		t.Fatalf("undeclared on a no-param view: got %v, want an error naming x", err)
	}
}

// TestRegistryGetAll proves lookup by name and the stable name-ordered listing.
func TestRegistryGetAll(t *testing.T) {
	b, a := minimalDef("b-view"), minimalDef("a-view")
	r, err := views.NewRegistry(b, a)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, ok := r.Get("a-view"); !ok {
		t.Errorf("Get(a-view): not found")
	}
	if _, ok := r.Get("nope"); ok {
		t.Errorf("Get(nope): found a view that was never registered")
	}
	all := r.All()
	if len(all) != 2 || all[0].Name != "a-view" || all[1].Name != "b-view" {
		t.Errorf("All() order = %v, want [a-view b-view]", []string{all[0].Name, all[1].Name})
	}
}

// TestRegistryRejects proves a misdeclared view fails registration loudly at
// construction (boot), never silently at run time: a duplicate name, an empty
// name, a missing permission, a missing scope resource, a field-mapping onto
// an absent column, and a param of an unknown type.
func TestRegistryRejects(t *testing.T) {
	cases := []struct {
		label string
		defs  []views.Definition
		names string // what the error must mention
	}{
		{"duplicate name", []views.Definition{minimalDef("dup"), minimalDef("dup")}, "dup"},
		{"empty name", []views.Definition{minimalDef("")}, "name"},
		{"no permission", []views.Definition{func() views.Definition {
			d := minimalDef("p")
			d.Permission = nil
			return d
		}()}, "permission"},
		{"no scope resource", []views.Definition{func() views.Definition {
			d := minimalDef("s")
			d.ScopeResource = ""
			return d
		}()}, "scope"},
		{"mapping onto absent column", []views.Definition{func() views.Definition {
			d := minimalDef("m")
			d.FieldMapping = map[string]string{"value": "missing"}
			return d
		}()}, "missing"},
		{"unknown param type", []views.Definition{func() views.Definition {
			d := minimalDef("t")
			d.Params = []views.Param{{Name: "x", Type: "float"}}
			return d
		}()}, "x"},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			_, err := views.NewRegistry(c.defs...)
			if err == nil {
				t.Fatalf("NewRegistry: want an error, got none")
			}
			if !strings.Contains(err.Error(), c.names) {
				t.Errorf("error %q does not mention %q", err.Error(), c.names)
			}
		})
	}
}

// TestDefaults proves the shipped default set registers cleanly and includes
// the proof view, component-reachability, with its declared permission and a
// complete field-mapping.
func TestDefaults(t *testing.T) {
	r, err := views.NewRegistry(views.Defaults()...)
	if err != nil {
		t.Fatalf("defaults must register: %v", err)
	}
	d, ok := r.Get("component-reachability")
	if !ok {
		t.Fatalf("component-reachability is not in the default set")
	}
	if strings.Join(d.Permission, ":") != "component:read" {
		t.Errorf("permission = %v, want component:read", d.Permission)
	}
	if d.ScopeResource != "component" {
		t.Errorf("scope resource = %q, want component", d.ScopeResource)
	}
	if d.FieldMapping["value"] == "" || d.FieldMapping["label"] == "" {
		t.Errorf("field-mapping must place value and label, got %v", d.FieldMapping)
	}
	if d.Run == nil {
		t.Errorf("the view has no Run")
	}
}
