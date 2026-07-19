package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// settingsFixture spins up a migrated, seeded gateway behind the live handler and
// mints an admin and a viewer token, so the settings routes are exercised end to
// end with realistically-granted principals.
type settingsFixture struct {
	c      *apiClient
	admin  string
	viewer string
}

func newSettingsFixture(t *testing.T) settingsFixture {
	t.Helper()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// An owner must exist so the role index (lazy) loads on the first authenticated
	// request; the admin and viewer principals carry the grants under test.
	bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	t.Cleanup(srv.Close)
	return settingsFixture{
		c:      &apiClient{t: t, ctx: ctx, base: srv.URL},
		admin:  principalWithGrants(t, ctx, dsn, "settings-admin", []grant{{role: "admin", scopeKind: "all"}}),
		viewer: principalWithGrants(t, ctx, dsn, "settings-viewer", []grant{{role: "viewer", scopeKind: "all"}}),
	}
}

// decodeSettings unmarshals a settings read body (values, sources, locks).
func decodeSettings(t *testing.T, raw []byte) struct {
	Values  map[string]map[string]any `json:"values"`
	Sources map[string]string         `json:"sources"`
	Locks   map[string]string         `json:"locks"`
} {
	t.Helper()
	var out struct {
		Values  map[string]map[string]any `json:"values"`
		Sources map[string]string         `json:"sources"`
		Locks   map[string]string         `json:"locks"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode settings body: %v (raw %s)", err, raw)
	}
	return out
}

func TestSettingsAdminReadReturnsProvenance(t *testing.T) {
	f := newSettingsFixture(t)
	raw := f.c.do(f.admin, http.MethodGet, "/settings", nil, http.StatusOK)
	body := decodeSettings(t, raw)
	if body.Values["ui"]["theme"] != "omniglass-dark" {
		t.Fatalf("effective ui.theme = %v, want default omniglass-dark", body.Values["ui"]["theme"])
	}
	if body.Sources["ui.theme"] != "code" {
		t.Fatalf("ui.theme source = %v, want code", body.Sources["ui.theme"])
	}
}

func TestSettingsPatchThenReadReflectsOverride(t *testing.T) {
	f := newSettingsFixture(t)
	f.c.do(f.admin, http.MethodPatch, "/settings/ui", map[string]any{"theme": "omniglass-light"}, http.StatusOK)
	raw := f.c.do(f.admin, http.MethodGet, "/settings", nil, http.StatusOK)
	body := decodeSettings(t, raw)
	if body.Values["ui"]["theme"] != "omniglass-light" {
		t.Fatalf("after patch ui.theme = %v, want omniglass-light", body.Values["ui"]["theme"])
	}
	if body.Sources["ui.theme"] != "global" {
		t.Fatalf("after patch source = %v, want global", body.Sources["ui.theme"])
	}
}

func TestSettingsMeIsReadableByViewer(t *testing.T) {
	f := newSettingsFixture(t)
	f.c.do(f.viewer, http.MethodGet, "/settings/me", nil, http.StatusOK)
}

func TestSettingsAdminReadForbiddenToViewer(t *testing.T) {
	f := newSettingsFixture(t)
	f.c.do(f.viewer, http.MethodGet, "/settings", nil, http.StatusForbidden)
}

// TestSettingsConcurrentPatchesNoLostUpdate fires many PATCHes at the same namespace
// at once, cycling across its distinct keys. A non-atomic read-modify-write loses
// updates: each PATCH reads the same stale doc, then the ON CONFLICT upsert overwrites
// the rest. An atomic gateway merge-patch serializes them so every key survives. The
// keys must be real fields of the namespace schema (the keybindings namespace has four
// free-string keys), else validation rejects the patch with 422 before the race is
// even reached.
func TestSettingsConcurrentPatchesNoLostUpdate(t *testing.T) {
	f := newSettingsFixture(t)
	keys := []string{"open_detail", "open_edit", "close_blade", "command_palette"}
	want := map[string]string{
		"open_detail":     "F1",
		"open_edit":       "F2",
		"close_blade":     "F3",
		"command_palette": "F4",
	}
	const n = 16

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := keys[i%len(keys)]
			body, _ := json.Marshal(map[string]any{key: want[key]})
			req, err := http.NewRequestWithContext(f.c.ctx, http.MethodPatch,
				f.c.base+"/api/v1/settings/keybindings", bytes.NewReader(body))
			if err != nil {
				t.Errorf("build request: %v", err)
				return
			}
			req.Header.Set("Authorization", "Bearer "+f.admin)
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Errorf("patch %s: %v", key, err)
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("patch %s = %d, want 200", key, resp.StatusCode)
			}
		}(i)
	}
	wg.Wait()

	raw := f.c.do(f.admin, http.MethodGet, "/settings", nil, http.StatusOK)
	body := decodeSettings(t, raw)
	for _, key := range keys {
		if got := body.Values["keybindings"][key]; got != want[key] {
			t.Fatalf("keybindings.%s = %v, want %s (a concurrent patch was lost)", key, got, want[key])
		}
	}
}

// TestSettingsPatchRejectsInvalidValue drives the schema-validation reject path end
// to end: a value outside a field's enum is refused with 422 before it is stored, so
// the typed schema (reflected from the Settings struct) is enforced at the HTTP edge,
// not only in the pure Validate unit.
func TestSettingsPatchRejectsInvalidValue(t *testing.T) {
	f := newSettingsFixture(t)
	f.c.do(f.admin, http.MethodPatch, "/settings/ui", map[string]any{"theme": "chartreuse"}, http.StatusUnprocessableEntity)
	// The rejected write left no override: the value still resolves to the default.
	raw := f.c.do(f.admin, http.MethodGet, "/settings", nil, http.StatusOK)
	if body := decodeSettings(t, raw); body.Values["ui"]["theme"] != "omniglass-dark" {
		t.Fatalf("after a rejected patch ui.theme = %v, want the unchanged default omniglass-dark", body.Values["ui"]["theme"])
	}
}

// TestSettingsPatchUnknownNamespaceIs404 confirms a write to a namespace that is not
// in the Settings struct is a 404, distinct from a 422 on a known namespace's bad key.
func TestSettingsPatchUnknownNamespaceIs404(t *testing.T) {
	f := newSettingsFixture(t)
	f.c.do(f.admin, http.MethodPatch, "/settings/bogus", map[string]any{"whatever": "x"}, http.StatusNotFound)
}
