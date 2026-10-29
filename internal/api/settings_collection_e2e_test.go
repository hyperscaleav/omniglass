package api_test

import (
	"net/http"
	"testing"
)

// The collection report window over the wire (#1369): how long each
// endpoint's collection telemetry window is, a platform-domain setting an
// admin writes and any authenticated caller reads.

// The default is a minute, as ruled: long enough that the telemetry costs a
// few rows per endpoint per minute, short enough to see a stall.
func TestCollectionReportWindowDefaultsToAMinute(t *testing.T) {
	f := newSettingsFixture(t)
	admin := decodeSettings(t, f.c.do(f.admin, http.MethodGet, "/settings", nil, http.StatusOK)).Values["collection"]
	viewer := decodeMe(t, f.c.do(f.viewer, http.MethodGet, "/settings/me", nil, http.StatusOK))["collection"]
	if admin["report_seconds"] != float64(60) || viewer["report_seconds"] != float64(60) {
		t.Errorf("collection.report_seconds = %v to an admin and %v to a viewer, want 60", admin["report_seconds"], viewer["report_seconds"])
	}
}

// Only an admin with install-wide authority changes the window, and only
// within ten seconds (below which a window holds about one heartbeat and the
// rows cost more than they say) and an hour (past which a stall goes unseen).
func TestCollectionReportWindowIsWritableOnlyByAnAdminAndInRange(t *testing.T) {
	f := newSettingsFixture(t)
	f.c.do(f.viewer, http.MethodPatch, "/settings/collection", map[string]any{"report_seconds": 30}, http.StatusForbidden)
	f.c.do(f.admin, http.MethodPatch, "/settings/collection", map[string]any{"report_seconds": 9}, http.StatusUnprocessableEntity)
	f.c.do(f.admin, http.MethodPatch, "/settings/collection", map[string]any{"report_seconds": 3601}, http.StatusUnprocessableEntity)
	f.c.do(f.admin, http.MethodPatch, "/settings/collection", map[string]any{"report_seconds": 30}, http.StatusOK)

	body := decodeSettings(t, f.c.do(f.admin, http.MethodGet, "/settings", nil, http.StatusOK))
	if body.Values["collection"]["report_seconds"] != float64(30) || body.Sources["collection.report_seconds"] != "platform" {
		t.Fatalf("collection.report_seconds = %v from %v, want 30 from platform", body.Values["collection"]["report_seconds"], body.Sources["collection.report_seconds"])
	}
}
