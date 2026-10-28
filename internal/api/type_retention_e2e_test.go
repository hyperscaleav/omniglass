package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// A type's own retention window over the wire (#417, PR 3): setRetention on a
// shipped type, which PATCH refuses as read-only, because how long an install
// keeps its samples is the install's. Gated like the retention settings (an
// admin with install-wide authority), bounded as they are, and cleared with
// null.
func TestATypesRetentionIsSetThroughItsOwnMethod(t *testing.T) {
	f := newSettingsFixture(t)
	read := func(path string) map[string]any {
		t.Helper()
		var body map[string]any
		if err := json.Unmarshal(f.c.do(f.admin, http.MethodGet, path, nil, http.StatusOK), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	f.c.do(f.viewer, http.MethodPost, "/metric-types/tcp-connect-time:setRetention", map[string]any{"days": 3}, http.StatusForbidden)
	// The Operator role may update a type, but a type's window is install-wide
	// policy, held to platform:update as the retention settings are: a window an
	// operator could set to one day would cut a shipped type to each series'
	// latest row on the next pass.
	f.c.do(f.operator, http.MethodPost, "/metric-types/tcp-connect-time:setRetention", map[string]any{"days": 1}, http.StatusForbidden)
	f.c.do(f.operator, http.MethodPost, "/property-types/endpoint-reachable:setRetention", map[string]any{"days": 1}, http.StatusForbidden)
	// An empty body is not a clear: only an explicit null returns a type to its
	// lane's window, so a bare command never deletes history by accident.
	f.c.do(f.admin, http.MethodPost, "/metric-types/tcp-connect-time:setRetention", map[string]any{}, http.StatusUnprocessableEntity)
	for _, bad := range []any{0, 36501, "3"} {
		f.c.do(f.admin, http.MethodPost, "/metric-types/tcp-connect-time:setRetention", map[string]any{"days": bad}, http.StatusUnprocessableEntity)
	}
	f.c.do(f.admin, http.MethodPost, "/metric-types/no-such-type:setRetention", map[string]any{"days": 3}, http.StatusNotFound)

	f.c.do(f.admin, http.MethodPost, "/metric-types/tcp-connect-time:setRetention", map[string]any{"days": 3}, http.StatusOK)
	if got := read("/metric-types/tcp-connect-time"); got["retention_days"] != float64(3) || got["official"] != true {
		t.Fatalf("tcp-connect-time = %v, want retention_days 3 on the official type", got)
	}
	f.c.do(f.admin, http.MethodPost, "/property-types/endpoint-reachable:setRetention", map[string]any{"days": 365}, http.StatusOK)
	if got := read("/property-types/endpoint-reachable"); got["retention_days"] != float64(365) {
		t.Fatalf("endpoint-reachable = %v, want retention_days 365", got)
	}

	f.c.do(f.admin, http.MethodPost, "/metric-types/tcp-connect-time:setRetention", map[string]any{"days": nil}, http.StatusOK)
	if got := read("/metric-types/tcp-connect-time"); got["retention_days"] != nil {
		t.Fatalf("after a null, tcp-connect-time = %v, want no retention_days (its lane's window)", got)
	}
}
