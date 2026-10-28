package api_test

import (
	"net/http"
	"testing"
)

// The retention windows over the wire (#417): a platform-domain namespace an
// admin writes, with the ruled defaults, that any authenticated caller reads.

// The defaults are the ruled windows: metric samples 14 days, property
// transitions 90 days, hourly trends 2 years. An admin reads them with their
// provenance, and any caller reads the effective windows, as a chart of a
// series' history would.
func TestRetentionWindowsDefaultToTheRuledDays(t *testing.T) {
	f := newSettingsFixture(t)
	admin := decodeSettings(t, f.c.do(f.admin, http.MethodGet, "/settings", nil, http.StatusOK)).Values["retention"]
	viewer := decodeMe(t, f.c.do(f.viewer, http.MethodGet, "/settings/me", nil, http.StatusOK))["retention"]
	for key, want := range map[string]float64{"metric_days": 14, "property_days": 90, "trend_days": 730} {
		if admin[key] != want || viewer[key] != want {
			t.Errorf("retention.%s = %v to an admin and %v to a viewer, want %v", key, admin[key], viewer[key], want)
		}
	}
}

// Only an admin with install-wide authority changes a window. A window of no
// days is refused rather than read as "delete everything", and so is one past a
// century: a cutoff that far back overflows the timestamp encoding and reaches
// the database as a date in the far future, which every row is older than.
func TestRetentionWindowsAreWritableOnlyByAnAdminAndInRange(t *testing.T) {
	f := newSettingsFixture(t)
	f.c.do(f.viewer, http.MethodPatch, "/settings/retention", map[string]any{"metric_days": 7}, http.StatusForbidden)
	f.c.do(f.admin, http.MethodPatch, "/settings/retention", map[string]any{"metric_days": 0}, http.StatusUnprocessableEntity)
	f.c.do(f.admin, http.MethodPatch, "/settings/retention", map[string]any{"metric_days": 36501}, http.StatusUnprocessableEntity)
	f.c.do(f.admin, http.MethodPatch, "/settings/retention", map[string]any{"metric_days": 999999999}, http.StatusUnprocessableEntity)
	f.c.do(f.admin, http.MethodPatch, "/settings/retention", map[string]any{"metric_days": 7}, http.StatusOK)

	body := decodeSettings(t, f.c.do(f.admin, http.MethodGet, "/settings", nil, http.StatusOK))
	if body.Values["retention"]["metric_days"] != float64(7) || body.Sources["retention.metric_days"] != "platform" {
		t.Fatalf("retention.metric_days = %v from %v, want 7 from platform", body.Values["retention"]["metric_days"], body.Sources["retention.metric_days"])
	}
}
