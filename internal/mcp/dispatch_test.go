package mcp

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/mcp/tooldef"
)

var widgetUpdate = tooldef.Def{
	Name: "update-widget", Method: "PATCH", Path: "/widgets/{name}/parts/{part}",
	PathParams: []string{"name", "part"}, QueryParams: []string{"dry_run"}, BodyParams: []string{"label", "dims"},
	InputSchema: `{"type":"object","required":["name","part"],"properties":{}}`,
}

func args(t *testing.T, s string) json.RawMessage {
	t.Helper()
	return json.RawMessage(s)
}

// Each argument lands on the part of the request it came from: a path
// parameter in its segment (escaped to stay one segment, as the CLI does), a
// query parameter in the query string, the rest in the JSON body.
func TestBuildRequestPlacesEachArgument(t *testing.T) {
	req, err := buildRequest(t.Context(), widgetUpdate, args(t, `{"name":"rack/a:b","part":"dial-1","dry_run":true,"label":"Dial","dims":{"w":2}}`))
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	if req.Method != "PATCH" {
		t.Errorf("method = %s", req.Method)
	}
	if got := req.URL.EscapedPath(); got != "/api/v1/widgets/rack%2Fa%3Ab/parts/dial-1" {
		t.Errorf("path = %s, want each path argument one escaped segment", got)
	}
	if got := req.URL.Query().Get("dry_run"); got != "true" {
		t.Errorf("query dry_run = %q", got)
	}
	body, _ := io.ReadAll(req.Body)
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("body %s: %v", body, err)
	}
	if got["label"] != "Dial" || got["dims"] == nil || len(got) != 2 {
		t.Errorf("body = %v, want only the body arguments", got)
	}
	if req.Header.Get("Content-Type") != "application/json" {
		t.Errorf("content type = %q", req.Header.Get("Content-Type"))
	}
}

// A read with no body sends none, and a free-form body travels whole.
func TestBuildRequestBodies(t *testing.T) {
	read := tooldef.Def{Name: "list-widgets", Method: "GET", Path: "/widgets", QueryParams: []string{"limit"}}
	req, err := buildRequest(t.Context(), read, nil)
	if err != nil || req.Body != nil && req.ContentLength != 0 {
		t.Fatalf("read request = %+v, %v; want no body", req, err)
	}
	patch := tooldef.Def{Name: "patch-ns", Method: "PATCH", Path: "/ns/{ns}", PathParams: []string{"ns"}, BodyParams: []string{"body"}, BodyWhole: true}
	req, err = buildRequest(t.Context(), patch, args(t, `{"ns":"ui","body":{"theme":"dark"}}`))
	if err != nil {
		t.Fatalf("whole body: %v", err)
	}
	b, _ := io.ReadAll(req.Body)
	if strings.TrimSpace(string(b)) != `{"theme":"dark"}` {
		t.Errorf("whole body = %s", b)
	}
}

// What would reach the wrong route, or silently drop an argument, is refused
// before anything is sent.
func TestBuildRequestRefuses(t *testing.T) {
	for name, a := range map[string]string{
		"an argument the tool does not take":    `{"name":"a","part":"b","colour":"red"}`,
		"a missing path argument":               `{"name":"a"}`,
		"a path argument that is not a string":  `{"name":7,"part":"b"}`,
		"a dot segment":                         `{"name":"..","part":"b"}`,
		"an empty path argument":                `{"name":"","part":"b"}`,
		"arguments that are not an object":      `["a","b"]`,
		"a query argument that is not a scalar": `{"name":"a","part":"b","dry_run":{"x":1}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := buildRequest(t.Context(), widgetUpdate, args(t, a)); err == nil {
				t.Errorf("buildRequest accepted %s", name)
			}
		})
	}
}
