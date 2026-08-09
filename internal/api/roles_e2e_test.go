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
	"github.com/jackc/pgx/v5"
)

// effectiveRoleWire is one decoded resolved role: the declaration, which arc it
// came from, and its staffing.
type effectiveRoleWire struct {
	Name           string   `json:"name"`
	DisplayName    string   `json:"display_name"`
	Quorum         int      `json:"quorum"`
	Capacity       *int     `json:"capacity"`
	PositionLabels []string `json:"position_labels"`
	Impact         string   `json:"impact"`
	Alternate      string   `json:"alternate"`
	AcceptedTypes  []string `json:"accepted_types"`
	PinnedProducts []string `json:"pinned_products"`
	FromStandard   bool     `json:"from_standard"`
	AssignedTo     []string `json:"assigned_to"`
	Assigned       int      `json:"assigned"`
	Understaffed   int      `json:"understaffed"`
}

type systemRolesWire struct {
	System string              `json:"system"`
	Roles  []effectiveRoleWire `json:"roles"`
}

func (w systemRolesWire) find(t *testing.T, name string) effectiveRoleWire {
	t.Helper()
	for _, r := range w.Roles {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("role %q not in the resolved read %+v", name, w.Roles)
	return effectiveRoleWire{}
}

// TestSystemRolesAPI drives the role surface over HTTP: a role declared on a
// standard resolves onto every conforming system with its staffing, a system
// declares its own alongside it, a component whose product's component_type
// falls within an accepted type fills it, and one that does not is refused
// with a 422 that names both parties in operator vocabulary (the whole point
// of the typed-slot model: a refusal an operator can act on). An out-of-scope
// system is a non-disclosing 404. Skipped under -short.
func TestSystemRolesAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ownerTok := bootstrapOwnerTok(t, ctx, gw)

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// A standard that wants a table mic (a video-bar, two of them), and a
	// system that conforms to it.
	c.do(ownerTok, http.MethodPost, "/standards", map[string]any{
		"name": "acme-room", "display_name": "Acme Room",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodPatch, "/standards/acme-room/roles/table-mic", map[string]any{
		"display_name": "Table Microphone", "quorum": 2,
		"accepted_types": []string{"video-bar"},
	}, http.StatusOK)
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{
		"name": "acme-1", "standard_id": "acme-room",
	}, http.StatusCreated)

	// The declaration read on the standard is the standard's own roles.
	var declared struct {
		Roles []struct {
			Name          string   `json:"name"`
			DisplayName   string   `json:"display_name"`
			Quorum        int      `json:"quorum"`
			AcceptedTypes []string `json:"accepted_types"`
		} `json:"roles"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/standards/acme-room/roles", nil, http.StatusOK), &declared); err != nil {
		t.Fatalf("decode standard roles: %v", err)
	}
	if len(declared.Roles) != 1 || declared.Roles[0].Name != "table-mic" ||
		declared.Roles[0].Quorum != 2 || len(declared.Roles[0].AcceptedTypes) != 1 || declared.Roles[0].AcceptedTypes[0] != "video-bar" {
		t.Fatalf("standard roles = %+v, want one table-mic wanting two, accepting video-bar", declared.Roles)
	}

	read := func(tok, name string) systemRolesWire {
		t.Helper()
		var w systemRolesWire
		if err := json.Unmarshal(c.do(tok, http.MethodGet, "/systems/"+name+"/roles", nil, http.StatusOK), &w); err != nil {
			t.Fatalf("decode system roles: %v", err)
		}
		return w
	}

	// The system inherits it, unstaffed: the read says how short it is.
	got := read(ownerTok, "acme-1")
	if got.System != "acme-1" || len(got.Roles) != 1 {
		t.Fatalf("resolved read = %+v, want acme-1 with the one inherited role", got)
	}
	mic := got.find(t, "table-mic")
	if !mic.FromStandard || mic.Quorum != 2 || mic.Assigned != 0 || mic.Understaffed != 2 {
		t.Fatalf("inherited table-mic = %+v, want from-standard, quorum 2, two short", mic)
	}

	// An ad-hoc role declared straight on the system sits beside the inherited
	// one, marked as not coming from the standard.
	c.do(ownerTok, http.MethodPatch, "/systems/acme-1/roles/wall-display", map[string]any{
		"display_name": "Wall Display", "accepted_types": []string{"display"},
	}, http.StatusOK)
	got = read(ownerTok, "acme-1")
	if len(got.Roles) != 2 {
		t.Fatalf("resolved read = %+v, want the inherited role plus the ad-hoc one", got.Roles)
	}
	if disp := got.find(t, "wall-display"); disp.FromStandard || disp.Quorum != 1 {
		t.Fatalf("wall-display = %+v, want ad-hoc with the default quorum of one", disp)
	}

	// A room bar is classified video-bar, so it fills the mic role.
	bar := "cisco-room-bar"
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "bar-1", "product": bar}, http.StatusCreated)
	c.do(ownerTok, http.MethodPut, "/systems/acme-1/roles/table-mic/assignments/bar-1", nil, http.StatusNoContent)
	mic = read(ownerTok, "acme-1").find(t, "table-mic")
	if mic.Assigned != 1 || mic.Understaffed != 1 || len(mic.AssignedTo) != 1 || mic.AssignedTo[0] != "bar-1" {
		t.Fatalf("after one assignment: %+v, want bar-1 filling it and still one short", mic)
	}

	// A display is not a video-bar: refused, and the refusal names both
	// parties. A bare 422 would leave the operator nothing to act on, so the
	// message is the contract, asserted here verbatim (the task brief's own
	// worked example, up to the specific names).
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "panel-1", "product": "samsung-qm55"}, http.StatusCreated)
	body := c.do(ownerTok, http.MethodPut, "/systems/acme-1/roles/table-mic/assignments/panel-1", nil, http.StatusUnprocessableEntity)
	const wantDetail = `component "panel-1" is a display; role "Table Microphone" wants a video-bar`
	var problem struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(body, &problem); err != nil {
		t.Fatalf("decode refusal: %v (%s)", err, body)
	}
	if !strings.Contains(problem.Detail, wantDetail) {
		t.Fatalf("refusal detail = %q, want it to name both parties: %q", problem.Detail, wantDetail)
	}

	// A second video-bar reaches quorum.
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "bar-2", "product": bar}, http.StatusCreated)
	c.do(ownerTok, http.MethodPut, "/systems/acme-1/roles/table-mic/assignments/bar-2", nil, http.StatusNoContent)
	mic = read(ownerTok, "acme-1").find(t, "table-mic")
	if mic.Assigned != 2 || mic.Understaffed != 0 {
		t.Fatalf("after staffing to quorum: %+v, want two assigned and no shortfall", mic)
	}

	// Unassigning leaves the role short again; unassigning twice is an explicit
	// miss.
	c.do(ownerTok, http.MethodDelete, "/systems/acme-1/roles/table-mic/assignments/bar-2", nil, http.StatusNoContent)
	if mic = read(ownerTok, "acme-1").find(t, "table-mic"); mic.Assigned != 1 || mic.Understaffed != 1 {
		t.Fatalf("after unassign: %+v, want one assigned and one short", mic)
	}
	c.do(ownerTok, http.MethodDelete, "/systems/acme-1/roles/table-mic/assignments/bar-2", nil, http.StatusNotFound)

	// A role nobody declared is a request fault rather than a 500.
	c.do(ownerTok, http.MethodPut, "/systems/acme-1/roles/no-such-role/assignments/bar-1", nil, http.StatusNotFound)

	// Withdrawing the ad-hoc role removes it from the resolved read; withdrawing
	// it twice is an explicit miss.
	c.do(ownerTok, http.MethodDelete, "/systems/acme-1/roles/wall-display", nil, http.StatusNoContent)
	if got = read(ownerTok, "acme-1"); len(got.Roles) != 1 {
		t.Fatalf("after withdrawing the ad-hoc role: %+v, want only the inherited one", got.Roles)
	}
	c.do(ownerTok, http.MethodDelete, "/systems/acme-1/roles/wall-display", nil, http.StatusNotFound)

	// Withdrawing the standard's role takes it off every conforming system, and
	// the assignments to it go with it.
	c.do(ownerTok, http.MethodDelete, "/standards/acme-room/roles/table-mic", nil, http.StatusNoContent)
	if got = read(ownerTok, "acme-1"); len(got.Roles) != 0 {
		t.Fatalf("after withdrawing the standard's role: %+v, want none left", got.Roles)
	}

	// A viewer scoped to another system reads its own roles but gets a
	// non-disclosing 404 on acme-1: the *:read floor passes the gate, scope
	// injection hides it.
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "other-sys"}, http.StatusCreated)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var otherID string
	if err := conn.QueryRow(ctx, `select id from system where name = 'other-sys'`).Scan(&otherID); err != nil {
		t.Fatalf("other id: %v", err)
	}
	viewerTok := setupScopedViewer(t, ctx, dsn, "viewer-other-roles", "viewer", "system", otherID)
	c.do(viewerTok, http.MethodGet, "/systems/acme-1/roles", nil, http.StatusNotFound)
	c.do(viewerTok, http.MethodGet, "/systems/other-sys/roles", nil, http.StatusOK)
}

// TestSeededStandardTypedRoles proves the ship-with example lands through the
// API on the typed-slot guard (#626, rework of the former
// TestSeededStandardRolesAPI): the meeting-room standard arrives with roles
// declaring accepted_types, not capabilities, so a system that conforms to it
// shows staffing work to do the moment it is created, the shipped catalog can
// actually satisfy what the shipped standard asks for, and a component of the
// wrong type is refused.
func TestSeededStandardTypedRoles(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{
		"name": "seeded-room", "standard_id": "meeting-room",
	}, http.StatusCreated)
	var w systemRolesWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/systems/seeded-room/roles", nil, http.StatusOK), &w); err != nil {
		t.Fatalf("decode system roles: %v", err)
	}
	if len(w.Roles) != 2 {
		t.Fatalf("seeded meeting-room roles = %+v, want the two the standard ships", w.Roles)
	}
	mic := w.find(t, "room-mic")
	if !mic.FromStandard || mic.Quorum != 2 || mic.Understaffed != 2 || !hasAllString(mic.AcceptedTypes, "video-bar") {
		t.Fatalf("seeded room-mic = %+v, want inherited, quorum 2, two short, accepting video-bar", mic)
	}
	if disp := w.find(t, "main-display"); !hasAllString(disp.AcceptedTypes, "display") {
		t.Fatalf("seeded main-display = %+v, want accepting display", disp)
	}

	// The shipped catalog can actually satisfy what the shipped standard asks
	// for: a Samsung QM55 fills the display role without any hand-declaration.
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "qm-1", "product": "samsung-qm55"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPut, "/systems/seeded-room/roles/main-display/assignments/qm-1", nil, http.StatusNoContent)

	// A touch panel is not a display: refused.
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "panel-9", "product": "crestron-tss-1070"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPut, "/systems/seeded-room/roles/main-display/assignments/panel-9", nil, http.StatusUnprocessableEntity)
}

// TestSwapPositionsAndCapacityRefusalsAPI drives the #626 position and
// capacity surfaces over HTTP: the ordered read reflects assignment order,
// :swapPositions exchanges two occupants in place, and the new refusals
// carry the status the architect ruling assigns them: 409 when the problem
// depends on other rows (double-staffing, a capacity too low for what is
// already assigned), 422 when the declaration alone is invalid (capacity
// below quorum).
func TestSwapPositionsAndCapacityRefusalsAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "swap-api-sys"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPatch, "/systems/swap-api-sys/roles/table-mic", map[string]any{
		"display_name": "Table Mic", "quorum": 2, "accepted_types": []string{"video-bar"},
	}, http.StatusOK)
	// No accepted_types: main-display takes any type, so the double-staffing
	// refusal below is isolated from the typed-slot guard.
	c.do(ownerTok, http.MethodPatch, "/systems/swap-api-sys/roles/main-display", map[string]any{
		"display_name": "Main Display",
	}, http.StatusOK)

	bar := "cisco-room-bar"
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "zeta", "product": bar}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "alpha", "product": bar}, http.StatusCreated)
	c.do(ownerTok, http.MethodPut, "/systems/swap-api-sys/roles/table-mic/assignments/zeta", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodPut, "/systems/swap-api-sys/roles/table-mic/assignments/alpha", nil, http.StatusNoContent)

	read := func() systemRolesWire {
		t.Helper()
		var w systemRolesWire
		if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/systems/swap-api-sys/roles", nil, http.StatusOK), &w); err != nil {
			t.Fatalf("decode system roles: %v", err)
		}
		return w
	}
	if got := read().find(t, "table-mic").AssignedTo; !hasAllString(got, "zeta", "alpha") || got[0] != "zeta" || got[1] != "alpha" {
		t.Fatalf("assigned_to before swap = %v, want [zeta alpha] (assignment order)", got)
	}

	c.do(ownerTok, http.MethodPost, "/systems/swap-api-sys/roles/table-mic:swapPositions",
		map[string]any{"position": 1, "with": 2}, http.StatusNoContent)
	if got := read().find(t, "table-mic").AssignedTo; got[0] != "alpha" || got[1] != "zeta" {
		t.Fatalf("assigned_to after swap = %v, want [alpha zeta]", got)
	}

	// A component already filling one role is refused (409, names both
	// parties) when assigned to a second in the same system.
	body := c.do(ownerTok, http.MethodPut, "/systems/swap-api-sys/roles/main-display/assignments/zeta", nil, http.StatusConflict)
	var problem struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(body, &problem); err != nil {
		t.Fatalf("decode refusal: %v (%s)", err, body)
	}
	if !strings.Contains(problem.Detail, "zeta") || !strings.Contains(problem.Detail, "Table Mic") {
		t.Fatalf("double-staffing refusal detail = %q, want it to name zeta and Table Mic", problem.Detail)
	}

	// A capacity below the role's own quorum is a 422 (the declaration alone
	// is invalid, independent of any other row): main-display has nobody
	// assigned yet, so this cannot also trip the below-assigned-count check.
	c.do(ownerTok, http.MethodPatch, "/systems/swap-api-sys/roles/main-display", map[string]any{
		"display_name": "Main Display", "quorum": 2, "accepted_types": []string{"display"}, "capacity": 1,
	}, http.StatusUnprocessableEntity)

	// A capacity below what is currently assigned is a 409 (it conflicts
	// with other rows), isolated from the quorum-alone check above by
	// staffing a third bar so the assigned count (3) exceeds quorum (2):
	// the proposed capacity of 2 clears "at least quorum" and fails only on
	// the assigned count.
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "gamma", "product": bar}, http.StatusCreated)
	c.do(ownerTok, http.MethodPut, "/systems/swap-api-sys/roles/table-mic/assignments/gamma", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodPatch, "/systems/swap-api-sys/roles/table-mic", map[string]any{
		"display_name": "Table Mic", "quorum": 2, "accepted_types": []string{"video-bar"}, "capacity": 2,
	}, http.StatusConflict)
}

// TestSetSystemRoleResolvesAmbiguousNameWithinCallerScope closes a gap Task
// 10's round 4 left: requireSystemInScope (roles.go) resolves its reference
// through gw.GetSystem, which after #627 can return storage.ErrAmbiguousName
// the moment two systems share a name, and that round added no test for it,
// because the behaviour-changing case needs the real migrated schema (a
// duplicate name is now legitimately reachable through an ordinary write
// path), not the constraint-dropped test fixture the earlier storage-layer
// duplicate-name tests use.
//
// The architect ruling ("scope decides before ambiguity does") means a
// caller scoped to one subtree, where the name is unique, must still
// succeed: only a caller who can see BOTH same-named rows is genuinely
// ambiguous. Both halves are asserted here, against the same route: the
// scoped deploy principal succeeds, and the all-scoped owner (who can see
// both "seat-1" rows) is refused with a 409, not a silent pick of either row
// and not a 500. Skipped under -short.
func TestSetSystemRoleResolvesAmbiguousNameWithinCallerScope(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ownerTok := bootstrapOwnerTok(t, ctx, gw)

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// A root system a scoped deploy principal will be granted, with a child
	// "seat-1" (the system_parent_name_key bucket).
	var scopeAV struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "scope-av"}, http.StatusCreated), &scopeAV); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "seat-1", "parent": "scope-av"}, http.StatusCreated)
	// An UNRELATED "seat-1" elsewhere (a root, the system_orphan_name_key
	// bucket), making the bare name ambiguous estate-wide.
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "seat-1"}, http.StatusCreated)

	// A deploy principal (system:update) scoped ONLY to scope-av's subtree.
	deployTok := setupScopedViewer(t, ctx, dsn, "deploy-av", "deploy", "system", scopeAV.ID)

	// Within scope-av's subtree, "seat-1" is unique (the other row sits
	// entirely outside it), so the scoped caller's declare succeeds even
	// though the bare name is ambiguous estate-wide.
	c.do(deployTok, http.MethodPatch, "/systems/seat-1/roles/table-mic", map[string]any{
		"display_name": "Table Mic", "quorum": 1,
	}, http.StatusOK)

	// The all-scoped owner, who CAN see both rows, is genuinely ambiguous.
	status, body := c.send(ownerTok, http.MethodPatch, "/systems/seat-1/roles/table-mic", map[string]any{
		"display_name": "Table Mic", "quorum": 1,
	})
	if status != http.StatusConflict {
		t.Fatalf("owner declare on ambiguous seat-1 status = %d, want 409\nbody: %s", status, body)
	}
}

// hasAllString is hasAll's API-test-package twin (the storage package's
// version lives in internal/storage, unexported).
func hasAllString(got []string, want ...string) bool {
	set := map[string]bool{}
	for _, g := range got {
		set[g] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}
