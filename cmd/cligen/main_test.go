package main

import (
	"strings"
	"testing"
)

// TestBuildCommandPathParams asserts that the generated request path substitutes
// path parameters as %s slots and binds them as positional args, in path order,
// for regular item paths, nested collections, and AIP :verb custom methods (the
// last is the regression: a {id}:archive segment must still bind the id).
func TestBuildCommandPathParams(t *testing.T) {
	const base = "/api/v1"
	cases := []struct {
		name     string
		path     string
		method   string
		wantPath string
		wantArgs []string
	}{
		{"item get", "/principals/{id}", "get", "/api/v1/principals/%s", []string{"id"}},
		{"collection list", "/principals", "get", "/api/v1/principals", nil},
		{"nested collection", "/principals/{id}/grants", "get", "/api/v1/principals/%s/grants", []string{"id"}},
		{"custom method archive", "/principals/{id}:archive", "post", "/api/v1/principals/%s:archive", []string{"id"}},
		{"custom method resetPassword", "/principals/{id}:resetPassword", "post", "/api/v1/principals/%s:resetPassword", []string{"id"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := buildCommand(spec{}, base, tc.path, tc.method, operation{})
			if cmd.APIPath != tc.wantPath {
				t.Errorf("APIPath = %q, want %q", cmd.APIPath, tc.wantPath)
			}
			if strings.Contains(cmd.APIPath, "{") {
				t.Errorf("APIPath %q still holds a literal path parameter", cmd.APIPath)
			}
			if len(cmd.Args) != len(tc.wantArgs) {
				t.Fatalf("Args = %v, want %v", cmd.Args, tc.wantArgs)
			}
			for i, a := range tc.wantArgs {
				if cmd.Args[i] != a {
					t.Errorf("Args[%d] = %q, want %q", i, cmd.Args[i], a)
				}
			}
		})
	}
}
