package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// declaredRoleWire is one role as its OWNER declares it: the write's echo and
// the standard-arc read, which is where a caller confirms what actually landed.
type declaredRoleWire struct {
	Name           string   `json:"name"`
	DisplayName    string   `json:"display_name"`
	Quorum         int      `json:"quorum"`
	Capacity       *int     `json:"capacity"`
	PositionLabels []string `json:"position_labels"`
	AcceptedTypes  []string `json:"accepted_types"`
	PinnedProducts []string `json:"pinned_products"`
	Impact         string   `json:"impact"`
	Alternate      string   `json:"alternate"`
}

// roleMaskFixture stands up an owner-token client over a standard that declares
// one fully specified role, the shape every case below patches.
func roleMaskFixture(t *testing.T) (*apiClient, string, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	t.Cleanup(srv.Close)
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	c.do(ownerTok, http.MethodPost, "/standards", map[string]any{
		"name": "mask-room", "display_name": "Mask Room",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodPatch, "/standards/mask-room/roles/main-display", map[string]any{
		"display_name": "Main Display", "quorum": 2, "capacity": 3, "impact": "outage",
		"position_labels": []string{"left", "right"},
		"accepted_types":  []string{"display"}, "pinned_products": []string{"samsung-qm55"},
	}, http.StatusOK)
	return c, ownerTok, "/standards/mask-room/roles/main-display"
}

// readDeclaredRole reads the role back off the standard's declaration list,
// which is the surface an operator confirms a write on.
func readDeclaredRole(t *testing.T, c *apiClient, tok, name string) declaredRoleWire {
	t.Helper()
	var list struct {
		Roles []declaredRoleWire `json:"roles"`
	}
	if err := json.Unmarshal(c.do(tok, http.MethodGet, "/standards/mask-room/roles", nil, http.StatusOK), &list); err != nil {
		t.Fatalf("decode standard roles: %v", err)
	}
	for _, r := range list.Roles {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("role %q not in the declaration read %+v", name, list.Roles)
	return declaredRoleWire{}
}

// TestRolePatchWithNoMaskLeavesOmittedFieldsAlone is #639 over the wire: the
// implied mask is the fields the body populated, so an edit that carries a
// display name changes a display name and nothing else. Before the conversion
// this route was a PUT that wholesale-replaced most of the row.
func TestRolePatchWithNoMaskLeavesOmittedFieldsAlone(t *testing.T) {
	c, tok, path := roleMaskFixture(t)

	c.do(tok, http.MethodPatch, path, map[string]any{"display_name": "Main Display (renamed)"}, http.StatusOK)

	got := readDeclaredRole(t, c, tok, "main-display")
	if got.DisplayName != "Main Display (renamed)" {
		t.Fatalf("display_name = %q, want the edit to take", got.DisplayName)
	}
	if got.Impact != "outage" {
		t.Fatalf("impact = %q, want outage: an omitted field is not a cleared field (#639)", got.Impact)
	}
	if got.Quorum != 2 || got.Capacity == nil || *got.Capacity != 3 {
		t.Fatalf("quorum/capacity = %d/%v, want 2/3 untouched", got.Quorum, got.Capacity)
	}
	if len(got.PositionLabels) != 2 || len(got.AcceptedTypes) != 1 || len(got.PinnedProducts) != 1 {
		t.Fatalf("lists after an unrelated edit = %+v, want all three intact (#639)", got)
	}
}

// TestRolePatchWithAMaskClearsCapacity is #638 over the wire, the worked case
// the primitive exists for: capacity is an integer, so it has no empty-string
// sentinel to clear it with, and under the implied mask alone "clear it" and
// "leave it alone" are the same request.
func TestRolePatchWithAMaskClearsCapacity(t *testing.T) {
	c, tok, path := roleMaskFixture(t)

	c.do(tok, http.MethodPatch, path, map[string]any{"update_mask": []string{"capacity"}}, http.StatusOK)

	got := readDeclaredRole(t, c, tok, "main-display")
	if got.Capacity != nil {
		t.Fatalf("capacity = %v, want it cleared back to unbounded (#638)", *got.Capacity)
	}
	if got.Impact != "outage" || got.Quorum != 2 || got.DisplayName != "Main Display" {
		t.Fatalf("role after a capacity-only mask = %+v, want everything the mask does not name untouched", got)
	}
}

// TestRolePatchWithAMaskClearsTheLists is the other half of the doc change the
// mask forces: an empty list is not a populated field, so clearing one means
// naming it in the mask, not sending [].
func TestRolePatchWithAMaskClearsTheLists(t *testing.T) {
	c, tok, path := roleMaskFixture(t)

	c.do(tok, http.MethodPatch, path, map[string]any{
		"accepted_types": []string{}, "position_labels": []string{},
	}, http.StatusOK)
	if got := readDeclaredRole(t, c, tok, "main-display"); len(got.AcceptedTypes) != 1 || len(got.PositionLabels) != 2 {
		t.Fatalf("role after sending empty lists = %+v, want them unchanged: an empty list is not populated", got)
	}

	c.do(tok, http.MethodPatch, path, map[string]any{
		"update_mask": []string{"accepted_types", "position_labels"},
	}, http.StatusOK)
	got := readDeclaredRole(t, c, tok, "main-display")
	if len(got.AcceptedTypes) != 0 || len(got.PositionLabels) != 0 {
		t.Fatalf("role after naming the lists in the mask = %+v, want both cleared", got)
	}
	if len(got.PinnedProducts) != 1 {
		t.Fatalf("pinned_products = %v, want the set the mask never named left alone", got.PinnedProducts)
	}
}

// TestRolePatchWithAStarMaskReplacesEverything proves "*" is the PUT this route
// used to be: every field the body omits goes back to its default.
func TestRolePatchWithAStarMaskReplacesEverything(t *testing.T) {
	c, tok, path := roleMaskFixture(t)

	c.do(tok, http.MethodPatch, path, map[string]any{
		"update_mask": []string{"*"}, "display_name": "Wiped",
	}, http.StatusOK)

	got := readDeclaredRole(t, c, tok, "main-display")
	if got.DisplayName != "Wiped" || got.Quorum != 1 || got.Capacity != nil || got.Impact != "degraded" {
		t.Fatalf("role after a full replacement = %+v, want every omitted field back to its default", got)
	}
	if len(got.AcceptedTypes) != 0 || len(got.PinnedProducts) != 0 || len(got.PositionLabels) != 0 {
		t.Fatalf("lists after a full replacement = %+v, want all three cleared", got)
	}
}

// TestRolePatchRefusesAMaskItCannotHonor: a mask naming a field the resource
// does not patch is a 422 that names the field, never a silent no-op, because
// a mask is the caller stating intent and a typo that succeeds is worse than
// one that fails.
func TestRolePatchRefusesAMaskItCannotHonor(t *testing.T) {
	c, tok, path := roleMaskFixture(t)

	for _, mask := range [][]string{{"colour"}, {"*", "quorum"}} {
		status, body := c.send(tok, http.MethodPatch, path, map[string]any{"update_mask": mask})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("patch with update_mask %v: status = %d, want 422\nbody: %s", mask, status, body)
		}
		var problem struct {
			Detail string `json:"detail"`
		}
		if err := json.Unmarshal(body, &problem); err != nil {
			t.Fatalf("decode refusal: %v (%s)", err, body)
		}
		if !strings.Contains(problem.Detail, mask[0]) {
			t.Fatalf("refusal detail = %q, want it to name %q", problem.Detail, mask[0])
		}
	}

	// The refusal is a refusal: nothing moved.
	if got := readDeclaredRole(t, c, tok, "main-display"); got.Impact != "outage" || got.Quorum != 2 {
		t.Fatalf("role after a refused mask = %+v, want it exactly as declared", got)
	}
}

// TestRoleAlternateRoundTrips is #640: the alternate was writable and read by
// nothing, so an operator could put a role into an alternate and had no API way
// to confirm it landed, and neither the generated client nor the CLI could
// round-trip it. Both role reads carry it now, and the write's own echo does
// too, so the same PATCH-then-GET that confirms every other field confirms this
// one.
func TestRoleAlternateRoundTrips(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	t.Cleanup(srv.Close)
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	c.do(ownerTok, http.MethodPost, "/standards", map[string]any{
		"name": "alt-room", "display_name": "Alternate Room",
	}, http.StatusCreated)
	// A choice has no write route of its own yet, so it arrives the way the
	// shipped standards' choices do.
	if _, err := gw.SeedRoleChoice(ctx, "standard", "alt-room", storage.RoleChoiceSpec{
		Name: "conferencing", DisplayName: "Conferencing",
		Alternates: []storage.AlternateSpec{
			{Name: "all-in-one", DisplayName: "All-in-one"},
			{Name: "component-system", DisplayName: "Component System"},
		},
	}); err != nil {
		t.Fatalf("seed choice: %v", err)
	}

	const path = "/standards/alt-room/roles/conf-bar"
	readAlt := func() string {
		t.Helper()
		var list struct {
			Roles []declaredRoleWire `json:"roles"`
		}
		if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/standards/alt-room/roles", nil, http.StatusOK), &list); err != nil {
			t.Fatalf("decode standard roles: %v", err)
		}
		for _, r := range list.Roles {
			if r.Name == "conf-bar" {
				return r.Alternate
			}
		}
		t.Fatalf("conf-bar not in the declaration read %+v", list.Roles)
		return ""
	}

	// The write's own echo answers first: a caller does not have to re-read to
	// see what it just joined.
	var echo declaredRoleWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPatch, path, map[string]any{
		"display_name": "Conferencing Bar", "accepted_types": []string{"video-bar"},
		"alternate": "conferencing/all-in-one",
	}, http.StatusOK), &echo); err != nil {
		t.Fatalf("decode declare echo: %v", err)
	}
	if echo.Alternate != "conferencing/all-in-one" {
		t.Fatalf("declare echo alternate = %q, want conferencing/all-in-one (#640)", echo.Alternate)
	}
	if got := readAlt(); got != "conferencing/all-in-one" {
		t.Fatalf("alternate on the declaration read = %q, want conferencing/all-in-one (#640)", got)
	}

	// And on the resolved per-system read, where an operator actually works.
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "alt-1", "standard_id": "alt-room"}, http.StatusCreated)
	var w systemRolesWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/systems/alt-1/roles", nil, http.StatusOK), &w); err != nil {
		t.Fatalf("decode system roles: %v", err)
	}
	if got := w.find(t, "conf-bar").Alternate; got != "conferencing/all-in-one" {
		t.Fatalf("alternate on the resolved read = %q, want conferencing/all-in-one (#640)", got)
	}

	// Moving between alternates, confirmed by reading it back.
	c.do(ownerTok, http.MethodPatch, path, map[string]any{"alternate": "conferencing/component-system"}, http.StatusOK)
	if got := readAlt(); got != "conferencing/component-system" {
		t.Fatalf("alternate after the move = %q, want conferencing/component-system", got)
	}

	// An unrelated edit leaves it alone (#626, now visible rather than taken on
	// trust), and an explicit empty string detaches.
	c.do(ownerTok, http.MethodPatch, path, map[string]any{"display_name": "Conferencing Bar (renamed)"}, http.StatusOK)
	if got := readAlt(); got != "conferencing/component-system" {
		t.Fatalf("alternate after an unrelated edit = %q, want it untouched", got)
	}
	c.do(ownerTok, http.MethodPatch, path, map[string]any{"alternate": ""}, http.StatusOK)
	if got := readAlt(); got != "" {
		t.Fatalf("alternate after an explicit detach = %q, want the role unconditional", got)
	}

	// And the mask reaches it like any other field: named with nothing to
	// carry, it clears.
	c.do(ownerTok, http.MethodPatch, path, map[string]any{"alternate": "conferencing/all-in-one"}, http.StatusOK)
	c.do(ownerTok, http.MethodPatch, path, map[string]any{"update_mask": []string{"alternate"}}, http.StatusOK)
	if got := readAlt(); got != "" {
		t.Fatalf("alternate after a mask that names it and carries nothing = %q, want it cleared", got)
	}
}

// TestSystemRolePatchTakesTheSameMask pins the system arc against the standard
// arc: the two share a body but not a handler, so a fix to one is not a fix to
// the other (#639 names both).
func TestSystemRolePatchTakesTheSameMask(t *testing.T) {
	c, tok, _ := roleMaskFixture(t)

	c.do(tok, http.MethodPost, "/systems", map[string]any{"name": "mask-sys"}, http.StatusCreated)
	const path = "/systems/mask-sys/roles/table-mic"
	c.do(tok, http.MethodPatch, path, map[string]any{
		"display_name": "Table Mic", "quorum": 2, "capacity": 4, "impact": "outage",
		"accepted_types": []string{"video-bar"},
	}, http.StatusOK)

	c.do(tok, http.MethodPatch, path, map[string]any{"display_name": "Table Mic (renamed)"}, http.StatusOK)
	c.do(tok, http.MethodPatch, path, map[string]any{"update_mask": []string{"capacity"}}, http.StatusOK)

	var w systemRolesWire
	if err := json.Unmarshal(c.do(tok, http.MethodGet, "/systems/mask-sys/roles", nil, http.StatusOK), &w); err != nil {
		t.Fatalf("decode system roles: %v", err)
	}
	mic := w.find(t, "table-mic")
	if mic.Capacity != nil {
		t.Fatalf("capacity = %v, want the system arc's mask to clear it too (#638)", *mic.Capacity)
	}
	if mic.Impact != "outage" || mic.Quorum != 2 || mic.DisplayName != "Table Mic (renamed)" {
		t.Fatalf("system-declared role = %+v, want the rename kept and impact and quorum untouched (#639)", mic)
	}
	if len(mic.AcceptedTypes) != 1 || mic.AcceptedTypes[0] != "video-bar" {
		t.Fatalf("accepted_types = %v, want the typed slot to survive both edits", mic.AcceptedTypes)
	}
}
