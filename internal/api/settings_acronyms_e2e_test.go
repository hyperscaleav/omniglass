package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The acronym list over the wire (#684): the first platform-domain namespace,
// and the first namespace that is platform-domain and client-visible at once.
// The pair is the point, so both halves are asserted here rather than inferred
// from the tag: an ordinary caller READS the effective list, and only an admin
// with install-wide authority WRITES it.

// decodeMe unmarshals the client-safe settings read, whose body is the typed
// document with no provenance.
func decodeMe(t *testing.T, raw []byte) map[string]map[string]any {
	t.Helper()
	var out struct {
		Values map[string]map[string]any `json:"values"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode /settings/me: %v (raw %s)", err, raw)
	}
	return out.Values
}

func acronymsOf(t *testing.T, values map[string]map[string]any) []string {
	t.Helper()
	raw, ok := values["label"]["acronyms"].([]any)
	if !ok {
		t.Fatalf("label.acronyms = %#v, want a list", values["label"]["acronyms"])
	}
	out := make([]string, len(raw))
	for i, v := range raw {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("acronym %d = %#v, want a string", i, v)
		}
		out[i] = s
	}
	return out
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// A viewer reads the effective list. The console renders a label from the same
// dictionary the server rendered it with, and it must be able to do that without
// the admin settings read, which is what client-visibility on a platform-domain
// namespace buys.
func TestAcronymListIsReadableByAnyAuthenticatedCaller(t *testing.T) {
	f := newSettingsFixture(t)
	got := acronymsOf(t, decodeMe(t, f.c.do(f.viewer, http.MethodGet, "/settings/me", nil, http.StatusOK)))
	if !contains(got, "DSP") {
		t.Fatalf("/settings/me acronyms = %v, want the shipped list including DSP", got)
	}
}

// Reading is not writing. A platform-domain namespace is admin-only to write
// whatever its client-visibility says, and the settings write routes carry
// platform:update on top of settings:update because an override is install-wide.
func TestAcronymListIsWritableOnlyByAnAdmin(t *testing.T) {
	f := newSettingsFixture(t)
	patch := map[string]any{"acronyms": []any{"AV", "QM55"}}
	f.c.do(f.viewer, http.MethodPatch, "/settings/label", patch, http.StatusForbidden)
	f.c.do(f.admin, http.MethodPatch, "/settings/label", patch, http.StatusOK)

	got := acronymsOf(t, decodeMe(t, f.c.do(f.viewer, http.MethodGet, "/settings/me", nil, http.StatusOK)))
	if len(got) != 2 || got[1] != "QM55" {
		t.Fatalf("after the admin write, acronyms = %v, want the operator's list", got)
	}
}

// The shipped list and an operator's stay distinguishable through the settings
// engine's own provenance: one key, and the level that supplied it answers "is
// this still what Omniglass ships?".
func TestAcronymProvenanceDistinguishesTheShippedListFromAnOperators(t *testing.T) {
	f := newSettingsFixture(t)
	body := decodeSettings(t, f.c.do(f.admin, http.MethodGet, "/settings", nil, http.StatusOK))
	if body.Sources["label.acronyms"] != "default" {
		t.Fatalf("untouched acronyms source = %q, want default", body.Sources["label.acronyms"])
	}

	f.c.do(f.admin, http.MethodPatch, "/settings/label", map[string]any{"acronyms": []any{"QM55"}}, http.StatusOK)

	body = decodeSettings(t, f.c.do(f.admin, http.MethodGet, "/settings", nil, http.StatusOK))
	if body.Sources["label.acronyms"] != "platform" {
		t.Fatalf("after the edit, acronyms source = %q, want platform", body.Sources["label.acronyms"])
	}

	// Restoring the namespace drops the override, and the shipped list returns:
	// the operator's edit layered over the declaration rather than destroying it.
	f.c.do(f.admin, http.MethodDelete, "/settings/label", nil, http.StatusNoContent)
	body = decodeSettings(t, f.c.do(f.admin, http.MethodGet, "/settings", nil, http.StatusOK))
	if body.Sources["label.acronyms"] != "default" {
		t.Fatalf("after restore, acronyms source = %q, want default", body.Sources["label.acronyms"])
	}
}

// A joined string is what a form sends when nobody taught it the field is a
// list, so the write validator has to refuse it at the surface (422) rather than
// storing a shape the typed decode fails on later.
func TestAcronymListRefusesAJoinedString(t *testing.T) {
	f := newSettingsFixture(t)
	f.c.do(f.admin, http.MethodPatch, "/settings/label",
		map[string]any{"acronyms": "AV,DSP"}, http.StatusUnprocessableEntity)
}
