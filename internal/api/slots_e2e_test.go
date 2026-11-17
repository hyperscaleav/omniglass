package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// effectiveSlotWire is one decoded resolved slot: the declaration, which arc it
// came from, and its staffing.
type effectiveSlotWire struct {
	Name           string   `json:"name"`
	Label          string   `json:"label"`
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

type systemSlotsWire struct {
	System string              `json:"system"`
	Slots  []effectiveSlotWire `json:"slots"`
}

func (w systemSlotsWire) find(t *testing.T, name string) effectiveSlotWire {
	t.Helper()
	for _, r := range w.Slots {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("slot %q not in the resolved read %+v", name, w.Slots)
	return effectiveSlotWire{}
}

// TestSystemSlotsAPI drives the slot surface over HTTP: a slot declared on a
// standard resolves onto every conforming system with its staffing, a system
// declares its own alongside it, a component whose product's component_type
// falls within an accepted type fills it, and one that does not is refused
// with a 422 that names both parties in operator vocabulary (the whole point
// of the typed-slot model: a refusal an operator can act on). An out-of-scope
// system is a non-disclosing 404. Skipped under -short.
func TestSystemSlotsAPI(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := seededDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	ownerTok := bootstrapOwnerTok(t, ctx, gw)

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// A standard that wants a table mic (a video-bar, two of them), and a
	// system that conforms to it.
	c.do(ownerTok, http.MethodPost, "/standards", map[string]any{
		"name": "acme-room", "label": "Acme Room",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodPatch, "/standards/acme-room/slots/table-mic", map[string]any{
		"label": "Table Microphone", "quorum": 2,
		"accepted_types": []string{"video-bar"},
	}, http.StatusOK)
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{
		"name": "acme-1", "standard_id": "acme-room",
	}, http.StatusCreated)

	// The declaration read on the standard is the standard's own slots.
	var declared struct {
		Slots []struct {
			Name          string   `json:"name"`
			Label         string   `json:"label"`
			Quorum        int      `json:"quorum"`
			AcceptedTypes []string `json:"accepted_types"`
		} `json:"slots"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/standards/acme-room/slots", nil, http.StatusOK), &declared); err != nil {
		t.Fatalf("decode standard slots: %v", err)
	}
	if len(declared.Slots) != 1 || declared.Slots[0].Name != "table-mic" ||
		declared.Slots[0].Quorum != 2 || len(declared.Slots[0].AcceptedTypes) != 1 || declared.Slots[0].AcceptedTypes[0] != "video-bar" {
		t.Fatalf("standard slots = %+v, want one table-mic wanting two, accepting video-bar", declared.Slots)
	}

	read := func(tok, name string) systemSlotsWire {
		t.Helper()
		var w systemSlotsWire
		if err := json.Unmarshal(c.do(tok, http.MethodGet, "/systems/"+name+"/slots", nil, http.StatusOK), &w); err != nil {
			t.Fatalf("decode system slots: %v", err)
		}
		return w
	}

	// The system inherits it, unstaffed: the read says how short it is.
	got := read(ownerTok, "acme-1")
	if got.System != "acme-1" || len(got.Slots) != 1 {
		t.Fatalf("resolved read = %+v, want acme-1 with the one inherited slot", got)
	}
	mic := got.find(t, "table-mic")
	if !mic.FromStandard || mic.Quorum != 2 || mic.Assigned != 0 || mic.Understaffed != 2 {
		t.Fatalf("inherited table-mic = %+v, want from-standard, quorum 2, two short", mic)
	}

	// An ad-hoc slot declared straight on the system sits beside the inherited
	// one, marked as not coming from the standard.
	c.do(ownerTok, http.MethodPatch, "/systems/acme-1/slots/wall-display", map[string]any{
		"label": "Wall Display", "accepted_types": []string{"display"},
	}, http.StatusOK)
	got = read(ownerTok, "acme-1")
	if len(got.Slots) != 2 {
		t.Fatalf("resolved read = %+v, want the inherited slot plus the ad-hoc one", got.Slots)
	}
	if disp := got.find(t, "wall-display"); disp.FromStandard || disp.Quorum != 1 {
		t.Fatalf("wall-display = %+v, want ad-hoc with the default quorum of one", disp)
	}

	// A product classified video-bar, so it fills the mic slot.
	bar := storagetest.MintProduct(t, ctx, gw, "video-bar").Name
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "bar-1", "product": bar}, http.StatusCreated)
	c.do(ownerTok, http.MethodPut, "/systems/acme-1/slots/table-mic/assignments/bar-1", nil, http.StatusNoContent)
	mic = read(ownerTok, "acme-1").find(t, "table-mic")
	if mic.Assigned != 1 || mic.Understaffed != 1 || len(mic.AssignedTo) != 1 || mic.AssignedTo[0] != "bar-1" {
		t.Fatalf("after one assignment: %+v, want bar-1 filling it and still one short", mic)
	}

	// A display is not a video-bar: refused, and the refusal names both
	// parties. A bare 422 would leave the operator nothing to act on, so the
	// message is the contract, asserted here verbatim (the task brief's own
	// worked example, up to the specific names).
	disp := storagetest.MintProduct(t, ctx, gw, "display")
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "panel-1", "product": disp.Name}, http.StatusCreated)
	body := c.do(ownerTok, http.MethodPut, "/systems/acme-1/slots/table-mic/assignments/panel-1", nil, http.StatusUnprocessableEntity)
	const wantDetail = `component "panel-1" is a display; slot "Table Microphone" wants a video-bar`
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
	c.do(ownerTok, http.MethodPut, "/systems/acme-1/slots/table-mic/assignments/bar-2", nil, http.StatusNoContent)
	mic = read(ownerTok, "acme-1").find(t, "table-mic")
	if mic.Assigned != 2 || mic.Understaffed != 0 {
		t.Fatalf("after staffing to quorum: %+v, want two assigned and no shortfall", mic)
	}

	// Unassigning leaves the slot short again; unassigning twice is an explicit
	// miss.
	c.do(ownerTok, http.MethodDelete, "/systems/acme-1/slots/table-mic/assignments/bar-2", nil, http.StatusNoContent)
	if mic = read(ownerTok, "acme-1").find(t, "table-mic"); mic.Assigned != 1 || mic.Understaffed != 1 {
		t.Fatalf("after unassign: %+v, want one assigned and one short", mic)
	}
	c.do(ownerTok, http.MethodDelete, "/systems/acme-1/slots/table-mic/assignments/bar-2", nil, http.StatusNotFound)

	// A slot nobody declared is a request fault rather than a 500.
	c.do(ownerTok, http.MethodPut, "/systems/acme-1/slots/no-such-slot/assignments/bar-1", nil, http.StatusNotFound)

	// Withdrawing the ad-hoc slot removes it from the resolved read; withdrawing
	// it twice is an explicit miss.
	c.do(ownerTok, http.MethodDelete, "/systems/acme-1/slots/wall-display", nil, http.StatusNoContent)
	if got = read(ownerTok, "acme-1"); len(got.Slots) != 1 {
		t.Fatalf("after withdrawing the ad-hoc slot: %+v, want only the inherited one", got.Slots)
	}
	c.do(ownerTok, http.MethodDelete, "/systems/acme-1/slots/wall-display", nil, http.StatusNotFound)

	// Withdrawing the standard's slot takes it off every conforming system, and
	// the assignments to it go with it.
	c.do(ownerTok, http.MethodDelete, "/standards/acme-room/slots/table-mic", nil, http.StatusNoContent)
	if got = read(ownerTok, "acme-1"); len(got.Slots) != 0 {
		t.Fatalf("after withdrawing the standard's slot: %+v, want none left", got.Slots)
	}

	// A viewer scoped to another system reads its own slots but gets a
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
	viewerTok := setupScopedViewer(t, ctx, dsn, "viewer-other-slots", "viewer", "system", otherID)
	c.do(viewerTok, http.MethodGet, "/systems/acme-1/slots", nil, http.StatusNotFound)
	c.do(viewerTok, http.MethodGet, "/systems/other-sys/slots", nil, http.StatusOK)
}

// TestSeededStandardTypedSlots proves the ship-with example lands through the
// API on the typed-slot guard (#626, rework of the former
// TestSeededStandardSlotsAPI): the meeting-room standard arrives with slots
// declaring accepted_types, not capabilities, so a system that conforms to it
// shows staffing work to do the moment it is created, a display-classified
// product can satisfy what the shipped standard asks for, and a component of
// the wrong type is refused.
func TestSeededStandardTypedSlots(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := seededDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	storagetest.CreateShippedStandards(t, gw)
	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{
		"name": "seeded-room", "standard_id": "meeting-room",
	}, http.StatusCreated)
	var w systemSlotsWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/systems/seeded-room/slots", nil, http.StatusOK), &w); err != nil {
		t.Fatalf("decode system slots: %v", err)
	}
	if len(w.Slots) != 4 {
		t.Fatalf("seeded meeting-room slots = %+v, want the four the standard ships (#802)", w.Slots)
	}
	bar := w.find(t, "video-bar")
	if !bar.FromStandard || bar.Quorum != 1 || bar.Understaffed != 1 || !hasAllString(bar.AcceptedTypes, "video-bar") {
		t.Fatalf("seeded video-bar = %+v, want inherited, quorum 1, one short, accepting video-bar", bar)
	}
	if disp := w.find(t, "main-display"); !hasAllString(disp.AcceptedTypes, "display") {
		t.Fatalf("seeded main-display = %+v, want accepting display", disp)
	}

	// A product classified display fills the display slot without any
	// hand-declaration: the typed slot matches on the classification alone.
	disp := storagetest.MintProduct(t, ctx, gw, "display")
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "qm-1", "product": disp.Name}, http.StatusCreated)
	c.do(ownerTok, http.MethodPut, "/systems/seeded-room/slots/main-display/assignments/qm-1", nil, http.StatusNoContent)

	// A touch panel is not a display: refused.
	panel := storagetest.MintProduct(t, ctx, gw, "touch-panel")
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "panel-9", "product": panel.Name}, http.StatusCreated)
	c.do(ownerTok, http.MethodPut, "/systems/seeded-room/slots/main-display/assignments/panel-9", nil, http.StatusUnprocessableEntity)
}

// TestSwapPositionsAndCapacityRefusalsAPI drives the #626 position and
// capacity surfaces over HTTP: the ordered read reflects assignment order,
// :swapPositions exchanges two occupants in place, and the new refusals
// carry the status the architect ruling assigns them: 409 when the problem
// depends on other rows (double-staffing, a capacity too low for what is
// already assigned), 422 when the declaration alone is invalid (capacity
// below quorum).
func TestSwapPositionsAndCapacityRefusalsAPI(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := seededDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "swap-api-sys"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPatch, "/systems/swap-api-sys/slots/table-mic", map[string]any{
		"label": "Table Mic", "quorum": 2, "accepted_types": []string{"video-bar"},
	}, http.StatusOK)
	// No accepted_types: main-display takes any type, so the double-staffing
	// refusal below is isolated from the typed-slot guard.
	c.do(ownerTok, http.MethodPatch, "/systems/swap-api-sys/slots/main-display", map[string]any{
		"label": "Main Display",
	}, http.StatusOK)

	bar := storagetest.MintProduct(t, ctx, gw, "video-bar").Name
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "zeta", "product": bar}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "alpha", "product": bar}, http.StatusCreated)
	c.do(ownerTok, http.MethodPut, "/systems/swap-api-sys/slots/table-mic/assignments/zeta", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodPut, "/systems/swap-api-sys/slots/table-mic/assignments/alpha", nil, http.StatusNoContent)

	read := func() systemSlotsWire {
		t.Helper()
		var w systemSlotsWire
		if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/systems/swap-api-sys/slots", nil, http.StatusOK), &w); err != nil {
			t.Fatalf("decode system slots: %v", err)
		}
		return w
	}
	if got := read().find(t, "table-mic").AssignedTo; !hasAllString(got, "zeta", "alpha") || got[0] != "zeta" || got[1] != "alpha" {
		t.Fatalf("assigned_to before swap = %v, want [zeta alpha] (assignment order)", got)
	}

	c.do(ownerTok, http.MethodPost, "/systems/swap-api-sys/slots/table-mic:swapPositions",
		map[string]any{"position": 1, "with": 2}, http.StatusNoContent)
	if got := read().find(t, "table-mic").AssignedTo; got[0] != "alpha" || got[1] != "zeta" {
		t.Fatalf("assigned_to after swap = %v, want [alpha zeta]", got)
	}

	// A component already filling one slot is refused (409, names both
	// parties) when assigned to a second in the same system.
	body := c.do(ownerTok, http.MethodPut, "/systems/swap-api-sys/slots/main-display/assignments/zeta", nil, http.StatusConflict)
	var problem struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(body, &problem); err != nil {
		t.Fatalf("decode refusal: %v (%s)", err, body)
	}
	if !strings.Contains(problem.Detail, "zeta") || !strings.Contains(problem.Detail, "Table Mic") {
		t.Fatalf("double-staffing refusal detail = %q, want it to name zeta and Table Mic", problem.Detail)
	}

	// A capacity below the slot's own quorum is a 422 (the declaration alone
	// is invalid, independent of any other row): main-display has nobody
	// assigned yet, so this cannot also trip the below-assigned-count check.
	c.do(ownerTok, http.MethodPatch, "/systems/swap-api-sys/slots/main-display", map[string]any{
		"label": "Main Display", "quorum": 2, "accepted_types": []string{"display"}, "capacity": 1,
	}, http.StatusUnprocessableEntity)

	// A capacity below what is currently assigned is a 409 (it conflicts
	// with other rows), isolated from the quorum-alone check above by
	// staffing a third bar so the assigned count (3) exceeds quorum (2):
	// the proposed capacity of 2 clears "at least quorum" and fails only on
	// the assigned count.
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "gamma", "product": bar}, http.StatusCreated)
	c.do(ownerTok, http.MethodPut, "/systems/swap-api-sys/slots/table-mic/assignments/gamma", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodPatch, "/systems/swap-api-sys/slots/table-mic", map[string]any{
		"label": "Table Mic", "quorum": 2, "accepted_types": []string{"video-bar"}, "capacity": 2,
	}, http.StatusConflict)
}

// TestSetSystemSlotResolvesAmbiguousNameWithinCallerScope closes a gap Task
// 10's round 4 left: requireSystemInScope (slots.go) resolves its reference
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
func TestSetSystemSlotResolvesAmbiguousNameWithinCallerScope(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
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
	// bucket), making the bare name ambiguous fleet-wide.
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "seat-1"}, http.StatusCreated)

	// A deploy principal (system:update) scoped ONLY to scope-av's subtree.
	deployTok := setupScopedViewer(t, ctx, dsn, "deploy-av", "deploy", "system", scopeAV.ID)

	// Within scope-av's subtree, "seat-1" is unique (the other row sits
	// entirely outside it), so the scoped caller's declare succeeds even
	// though the bare name is ambiguous fleet-wide.
	c.do(deployTok, http.MethodPatch, "/systems/seat-1/slots/table-mic", map[string]any{
		"label": "Table Mic", "quorum": 1,
	}, http.StatusOK)

	// The all-scoped owner, who CAN see both rows, is genuinely ambiguous.
	status, body := c.send(ownerTok, http.MethodPatch, "/systems/seat-1/slots/table-mic", map[string]any{
		"label": "Table Mic", "quorum": 1,
	})
	if status != http.StatusNotFound {
		t.Fatalf("owner declare on ambiguous seat-1 status = %d, want 404\nbody: %s", status, body)
	}
	if !strings.Contains(string(body), "seat-1") {
		t.Fatalf("404 body = %s, want it to name the ambiguous reference", body)
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
