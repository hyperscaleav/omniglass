package api_test

import (
	"net/http"
	"testing"
)

// The retention windows over the wire (#417): a platform-domain namespace an
// admin writes, with the ruled defaults, that any authenticated caller reads.

// The defaults are the ruled windows (metric samples 14 days, property
// transitions 90 days, hourly trends 2 years), and the log bands' (debug 3,
// info 14, warning 30, error 90). An admin reads them with their
// provenance, and any caller reads the effective windows, as a chart of a
// series' history would.
func TestRetentionWindowsDefaultToTheRuledDays(t *testing.T) {
	f := newSettingsFixture(t)
	admin := decodeSettings(t, f.c.do(f.admin, http.MethodGet, "/settings", nil, http.StatusOK)).Values["retention"]
	viewer := decodeMe(t, f.c.do(f.viewer, http.MethodGet, "/settings/me", nil, http.StatusOK))["retention"]
	for key, want := range map[string]float64{"metric_days": 14, "property_days": 90, "trend_days": 730,
		"log_debug_days": 3, "log_info_days": 14, "log_warning_days": 30, "log_error_days": 90} {
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

// A node's own log windows (#1350) are unset by default: the effective
// document leaves them out, so node_log follows the shared bands. An admin sets
// one in the same range as every window, and restoring it (a null) removes it
// again.
func TestNodeLogRetentionOverrideIsUnsetUntilAnAdminSetsIt(t *testing.T) {
	f := newSettingsFixture(t)
	keys := []string{"node_log_debug_days", "node_log_info_days", "node_log_warning_days", "node_log_error_days"}
	admin := decodeSettings(t, f.c.do(f.admin, http.MethodGet, "/settings", nil, http.StatusOK)).Values["retention"]
	for _, key := range keys {
		if v, ok := admin[key]; ok {
			t.Errorf("retention.%s = %v by default, want it unset", key, v)
		}
	}
	for _, key := range keys {
		f.c.do(f.viewer, http.MethodPatch, "/settings/retention", map[string]any{key: 7}, http.StatusForbidden)
		f.c.do(f.admin, http.MethodPatch, "/settings/retention", map[string]any{key: 0}, http.StatusUnprocessableEntity)
		f.c.do(f.admin, http.MethodPatch, "/settings/retention", map[string]any{key: 36501}, http.StatusUnprocessableEntity)
	}
	f.c.do(f.admin, http.MethodPatch, "/settings/retention", map[string]any{"node_log_debug_days": 1}, http.StatusOK)
	body := decodeSettings(t, f.c.do(f.admin, http.MethodGet, "/settings", nil, http.StatusOK))
	if body.Values["retention"]["node_log_debug_days"] != float64(1) || body.Sources["retention.node_log_debug_days"] != "platform" {
		t.Fatalf("retention.node_log_debug_days = %v from %v, want 1 from platform",
			body.Values["retention"]["node_log_debug_days"], body.Sources["retention.node_log_debug_days"])
	}
	if body.Values["retention"]["log_debug_days"] != float64(3) {
		t.Errorf("retention.log_debug_days = %v after a node override, want the shared 3 untouched", body.Values["retention"]["log_debug_days"])
	}
	f.c.do(f.admin, http.MethodPatch, "/settings/retention", map[string]any{"node_log_debug_days": nil}, http.StatusOK)
	body = decodeSettings(t, f.c.do(f.admin, http.MethodGet, "/settings", nil, http.StatusOK))
	if v, ok := body.Values["retention"]["node_log_debug_days"]; ok {
		t.Errorf("retention.node_log_debug_days = %v after a restore, want it unset again", v)
	}
}
