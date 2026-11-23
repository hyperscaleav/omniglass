package api_test

import (
	"net/http"
	"testing"
)

// TestSettingsMePatchIsTheCallersOwn drives the user level end to end (#2450):
// a viewer, who may not touch a platform setting, marks a lesson done through
// PATCH /settings/me/learn and reads it back from /settings/me; another
// person's /settings/me is untouched by it, and the platform read is too.
func TestSettingsMePatchIsTheCallersOwn(t *testing.T) {
	t.Parallel()
	f := newSettingsFixture(t)

	raw := f.c.do(f.viewer, http.MethodPatch, "/settings/me/learn",
		map[string]any{"done": []string{"device-to-samples"}}, http.StatusOK)
	if got := decodeSettings(t, raw).Values["learn"]["done"]; len(got.([]any)) != 1 {
		t.Fatalf("patch answer learn.done = %v, want the one lesson", got)
	}
	me := decodeSettings(t, f.c.do(f.viewer, http.MethodGet, "/settings/me", nil, http.StatusOK))
	if done, _ := me.Values["learn"]["done"].([]any); len(done) != 1 || done[0] != "device-to-samples" {
		t.Fatalf("viewer learn.done = %v, want [device-to-samples]", me.Values["learn"]["done"])
	}
	other := decodeSettings(t, f.c.do(f.operator, http.MethodGet, "/settings/me", nil, http.StatusOK))
	if done, _ := other.Values["learn"]["done"].([]any); len(done) != 0 {
		t.Fatalf("operator learn.done = %v, want none: the viewer's progress is the viewer's", done)
	}
	admin := decodeSettings(t, f.c.do(f.admin, http.MethodGet, "/settings", nil, http.StatusOK))
	if done, _ := admin.Values["learn"]["done"].([]any); len(done) != 0 {
		t.Fatalf("platform learn.done = %v, want none: a user binding is not the platform's", done)
	}
}

// TestSettingsMePatchRefusesPlatformDomainAndBadValues: a person binds only a
// profile-domain namespace (a platform one is the admin's, 403), an unknown
// namespace is a 404, and a value the schema refuses is a 422.
func TestSettingsMePatchRefusesPlatformDomainAndBadValues(t *testing.T) {
	t.Parallel()
	f := newSettingsFixture(t)
	f.c.do(f.viewer, http.MethodPatch, "/settings/me/retention", map[string]any{"metric_days": 3}, http.StatusForbidden)
	f.c.do(f.viewer, http.MethodPatch, "/settings/me/nope", map[string]any{"x": 1}, http.StatusNotFound)
	f.c.do(f.viewer, http.MethodPatch, "/settings/me/ui", map[string]any{"theme": "neon"}, http.StatusUnprocessableEntity)
}

// TestSettingsMePatchCannotShadowAPlatformLock: a key an admin locked at the
// platform level is refused at the user level (409) rather than stored and
// silently ignored by the cascade.
func TestSettingsMePatchCannotShadowAPlatformLock(t *testing.T) {
	t.Parallel()
	f := newSettingsFixture(t)
	if _, err := f.gw.UpsertSettingBinding(f.c.ctx, "", "platform", "ui",
		map[string]any{"theme": "omniglass-dark"}, []string{"theme"}); err != nil {
		t.Fatalf("lock theme: %v", err)
	}
	f.c.do(f.viewer, http.MethodPatch, "/settings/me/ui", map[string]any{"theme": "omniglass-light"}, http.StatusConflict)
	f.c.do(f.viewer, http.MethodPatch, "/settings/me/ui", map[string]any{"default_landing": "/alarms"}, http.StatusOK)
}
