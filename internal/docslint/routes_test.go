package docslint

import (
	"strings"
	"testing"
)

func routeUniverse() map[string]map[string]bool {
	return map[string]map[string]bool{
		"/components":                   {"get": true, "post": true},
		"/components/{}":                {"get": true, "patch": true, "delete": true},
		"/audit-log":                    {"get": true},
		"/auth/me:changePassword":       {"post": true},
		"/healthz":                      {"get": true},
		"/components/{}/commands:issue": {"post": true},
		"/components/{}:setTag":         {"post": true},
		"/systems/{}:setTag":            {"post": true},
	}
}

// TestScanRoutesResolves pins the extraction and matching rules one case at a
// time: method-prefixed mentions, bare route-shaped code spans, placeholder
// normalization, the /api/v1 prefix, and the signals that keep prose paths
// (docs links, file paths) out of the lint.
func TestScanRoutesResolves(t *testing.T) {
	cases := []struct {
		name string
		line string
		want int
	}{
		{"a real method+path resolves", "list with `GET /components`", 0},
		{"an unknown path fires", "call `GET /nonexistent` to list", 1},
		{"a method the path lacks fires", "`DELETE /audit-log` prunes it", 1},
		{"a placeholder normalizes", "`PATCH /components/{name}` edits", 0},
		{"a custom :verb resolves", "`POST /components/{name}/commands:issue`", 0},
		{"a bare route-shaped span resolves", "the session drop is `/auth/me:changePassword`", 0},
		{"a bare span with a placeholder fires when unknown", "an operation row at `/actions/{id}`", 1},
		{"the /api/v1 prefix strips", "probe `GET /api/v1/healthz`", 0},
		{"a docs-site link path is not a route", "see `/architecture/storage/` for the tables", 0},
		{"a file path is not a route", "the gateway is `internal/storage/gateway.go`", 0},
		{"a bare unknown resource without signals is skipped", "the `/foobars` idea", 0},
		{"a one-segment span is a family mention", "the `/auth` family", 0},
		{"comma shorthand expands and resolves", "`POST /{components,systems}/{name}:setTag`", 0},
		{"comma shorthand fires per missing alternative", "`POST /{components,ghosts}/{name}:setTag`", 1},
		{"an ellipsis span is deliberate shorthand", "`/types/...` covers the rest", 0},
		{"a query string strips before the check", "`GET /components?filter=x`", 0},
		{"an illustrative mention is a teaching stand-in", "P calls the illustrative `POST /ghosts/X:scare`:", 0},
	}
	routes := routeUniverse()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := scanRoutesIn(c.line, routes)
			if len(got) != c.want {
				t.Fatalf("scanRoutesIn(%q) = %v, want %d finding(s)", c.line, got, c.want)
			}
		})
	}
}

// TestScanRoutesIsFenceAware pins the #434 contract: a route documented inside
// a :::design fence is target design and exempt from the existence lint.
func TestScanRoutesIsFenceAware(t *testing.T) {
	doc := strings.Join([]string{
		"`GET /components` is built.",
		"",
		":::design[#434]",
		"`GET /actions/{id}` will poll the operation row.",
		":::",
		"",
		"but `GET /ghosts` claims to exist.",
	}, "\n")
	got := scanRoutesInDoc(doc, routeUniverse())
	if len(got) != 1 || !strings.Contains(got[0].Text, "/ghosts") {
		t.Fatalf("want exactly the unfenced ghost route, got %v", got)
	}
	if got[0].Line != 7 {
		t.Fatalf("finding on line %d, want 7", got[0].Line)
	}
}

// TestScanRoutesReadsHTTPExamples pins that a request line inside a fenced
// code block is checked too; wrong routes in curl/http examples are exactly
// the drift a reader copies.
func TestScanRoutesReadsHTTPExamples(t *testing.T) {
	doc := strings.Join([]string{
		"```",
		"GET /components",
		"GET /nonexistent",
		"```",
	}, "\n")
	got := scanRoutesInDoc(doc, routeUniverse())
	if len(got) != 1 || !strings.Contains(got[0].Text, "/nonexistent") {
		t.Fatalf("want exactly the bad example line, got %v", got)
	}
}

// TestRouteReferences runs the route lint over the real corpus, enforced: a
// documented route resolves against the OpenAPI document or sits inside a
// design fence.
func TestRouteReferences(t *testing.T) {
	findings, err := ScanRouteReferences()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	report(t, "route-reference", findings, true)
}
