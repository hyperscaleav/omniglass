package api_test

import (
	"net/http"
	"testing"
)

// The platform's collection cadences (#1373): how often a reachability probe
// runs, and how often a driver poll that declares no schedule runs. Both
// default to the product's heartbeat, so an install that sets nothing behaves
// as it always has, and both are refused below it: the heartbeat is the
// granularity a node runs at (ADR-0152), so a faster cadence would be a
// promise no node keeps.
func TestCollectionCadencesDefaultToTheHeartbeatAndRefuseBelowIt(t *testing.T) {
	f := newSettingsFixture(t)
	admin := decodeSettings(t, f.c.do(f.admin, http.MethodGet, "/settings", nil, http.StatusOK)).Values["collection"]
	if admin["probe_seconds"] != float64(30) || admin["poll_seconds"] != float64(30) {
		t.Fatalf("collection probe_seconds %v, poll_seconds %v, want 30 and 30", admin["probe_seconds"], admin["poll_seconds"])
	}
	for _, key := range []string{"probe_seconds", "poll_seconds"} {
		f.c.do(f.viewer, http.MethodPatch, "/settings/collection", map[string]any{key: 60}, http.StatusForbidden)
		f.c.do(f.admin, http.MethodPatch, "/settings/collection", map[string]any{key: 29}, http.StatusUnprocessableEntity)
		f.c.do(f.admin, http.MethodPatch, "/settings/collection", map[string]any{key: 86401}, http.StatusUnprocessableEntity)
		f.c.do(f.admin, http.MethodPatch, "/settings/collection", map[string]any{key: 120}, http.StatusOK)
	}
	body := decodeSettings(t, f.c.do(f.admin, http.MethodGet, "/settings", nil, http.StatusOK))
	if body.Values["collection"]["probe_seconds"] != float64(120) || body.Sources["collection.probe_seconds"] != "platform" {
		t.Fatalf("collection.probe_seconds = %v from %v, want 120 from platform", body.Values["collection"]["probe_seconds"], body.Sources["collection.probe_seconds"])
	}
}
