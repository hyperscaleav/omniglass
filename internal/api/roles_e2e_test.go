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
	Name         string   `json:"name"`
	DisplayName  string   `json:"display_name"`
	Quorum       int      `json:"quorum"`
	Capabilities []string `json:"capabilities"`
	FromStandard bool     `json:"from_standard"`
	AssignedTo   []string `json:"assigned_to"`
	Assigned     int      `json:"assigned"`
	Understaffed int      `json:"understaffed"`
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
// declares its own alongside it, a component that provides what the role needs
// fills it, and one that does not is refused with a 422 that NAMES the missing
// capabilities (the whole point of the capability model: a refusal an operator
// can act on). A productless component that declares its own capabilities can
// still be staffed. An out-of-scope system is a non-disclosing 404. Skipped
// under -short.
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

	// A standard that wants a table mic (microphone AND speaker, two of them),
	// and a system that conforms to it.
	c.do(ownerTok, http.MethodPost, "/standards", map[string]any{
		"id": "acme-room", "display_name": "Acme Room",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodPut, "/standards/acme-room/roles/table-mic", map[string]any{
		"display_name": "Table Microphone", "quorum": 2,
		"capabilities": []string{"microphone", "speaker"},
	}, http.StatusOK)
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{
		"name": "acme-1", "standard_id": "acme-room",
	}, http.StatusCreated)

	// The declaration read on the standard is the standard's own roles.
	var declared struct {
		Roles []struct {
			Name         string   `json:"name"`
			DisplayName  string   `json:"display_name"`
			Quorum       int      `json:"quorum"`
			Capabilities []string `json:"capabilities"`
		} `json:"roles"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/standards/acme-room/roles", nil, http.StatusOK), &declared); err != nil {
		t.Fatalf("decode standard roles: %v", err)
	}
	if len(declared.Roles) != 1 || declared.Roles[0].Name != "table-mic" ||
		declared.Roles[0].Quorum != 2 || len(declared.Roles[0].Capabilities) != 2 {
		t.Fatalf("standard roles = %+v, want one table-mic wanting two, requiring two capabilities", declared.Roles)
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
	c.do(ownerTok, http.MethodPut, "/systems/acme-1/roles/wall-display", map[string]any{
		"display_name": "Wall Display", "capabilities": []string{"flat-panel-display"},
	}, http.StatusOK)
	got = read(ownerTok, "acme-1")
	if len(got.Roles) != 2 {
		t.Fatalf("resolved read = %+v, want the inherited role plus the ad-hoc one", got.Roles)
	}
	if disp := got.find(t, "wall-display"); disp.FromStandard || disp.Quorum != 1 {
		t.Fatalf("wall-display = %+v, want ad-hoc with the default quorum of one", disp)
	}

	// A room bar provides microphone and speaker, so it can fill the mic role.
	bar := "cisco-room-bar"
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "bar-1", "product": bar}, http.StatusCreated)
	c.do(ownerTok, http.MethodPut, "/systems/acme-1/roles/table-mic/assignments/bar-1", nil, http.StatusNoContent)
	mic = read(ownerTok, "acme-1").find(t, "table-mic")
	if mic.Assigned != 1 || mic.Understaffed != 1 || len(mic.AssignedTo) != 1 || mic.AssignedTo[0] != "bar-1" {
		t.Fatalf("after one assignment: %+v, want bar-1 filling it and still one short", mic)
	}

	// A display provides neither: refused, and the refusal names the gap. A bare
	// 422 would leave the operator nothing to act on, so the message is the
	// contract, asserted here verbatim.
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "panel-1", "product": "samsung-qm55"}, http.StatusCreated)
	body := c.do(ownerTok, http.MethodPut, "/systems/acme-1/roles/table-mic/assignments/panel-1", nil, http.StatusUnprocessableEntity)
	const wantDetail = `component "panel-1" cannot fill role "table-mic": missing microphone, speaker`
	var problem struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(body, &problem); err != nil {
		t.Fatalf("decode refusal: %v (%s)", err, body)
	}
	if !strings.Contains(problem.Detail, wantDetail) {
		t.Fatalf("refusal detail = %q, want it to name the missing capabilities: %q", problem.Detail, wantDetail)
	}

	// The decision the capability model exists for: a PRODUCTLESS component that
	// declares its own capabilities can be staffed, so strict refusal does not
	// lock out a component just because it has no product.
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "loose-mic"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPut, "/components/loose-mic/capabilities/microphone",
		map[string]any{"present": true}, http.StatusNoContent)
	c.do(ownerTok, http.MethodPut, "/components/loose-mic/capabilities/speaker",
		map[string]any{"present": true}, http.StatusNoContent)
	if caps := readCapabilities(t, c, ownerTok, "loose-mic"); len(caps) != 2 || caps[0] != "microphone" || caps[1] != "speaker" {
		t.Fatalf("productless capabilities = %v, want exactly [microphone speaker]", caps)
	}
	c.do(ownerTok, http.MethodPut, "/systems/acme-1/roles/table-mic/assignments/loose-mic", nil, http.StatusNoContent)
	mic = read(ownerTok, "acme-1").find(t, "table-mic")
	if mic.Assigned != 2 || mic.Understaffed != 0 {
		t.Fatalf("after staffing to quorum: %+v, want two assigned and no shortfall", mic)
	}

	// Suppressing a capability the product declares takes the component back out
	// of contention for anything that needs it.
	c.do(ownerTok, http.MethodPut, "/components/panel-1/capabilities/flat-panel-display",
		map[string]any{"present": false}, http.StatusNoContent)
	if caps := readCapabilities(t, c, ownerTok, "panel-1"); len(caps) != 0 {
		t.Fatalf("suppressed capabilities = %v, want none left", caps)
	}
	// Clearing the fact falls back to the product's set; clearing it twice is an
	// explicit miss.
	c.do(ownerTok, http.MethodDelete, "/components/panel-1/capabilities/flat-panel-display", nil, http.StatusNoContent)
	if caps := readCapabilities(t, c, ownerTok, "panel-1"); len(caps) != 1 || caps[0] != "flat-panel-display" {
		t.Fatalf("cleared capabilities = %v, want the product's set back", caps)
	}
	c.do(ownerTok, http.MethodDelete, "/components/panel-1/capabilities/flat-panel-display", nil, http.StatusNotFound)

	// Unassigning leaves the role short again; unassigning twice is an explicit
	// miss.
	c.do(ownerTok, http.MethodDelete, "/systems/acme-1/roles/table-mic/assignments/loose-mic", nil, http.StatusNoContent)
	if mic = read(ownerTok, "acme-1").find(t, "table-mic"); mic.Assigned != 1 || mic.Understaffed != 1 {
		t.Fatalf("after unassign: %+v, want one assigned and one short", mic)
	}
	c.do(ownerTok, http.MethodDelete, "/systems/acme-1/roles/table-mic/assignments/loose-mic", nil, http.StatusNotFound)

	// A role nobody declared, and a capability the registry does not know, are
	// request faults rather than 500s.
	c.do(ownerTok, http.MethodPut, "/systems/acme-1/roles/no-such-role/assignments/bar-1", nil, http.StatusNotFound)
	c.do(ownerTok, http.MethodPut, "/components/bar-1/capabilities/not-a-capability",
		map[string]any{"present": true}, http.StatusUnprocessableEntity)

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

// readCapabilities decodes a component's resolved capability set.
func readCapabilities(t *testing.T, c *apiClient, tok, name string) []string {
	t.Helper()
	var w struct {
		Component    string   `json:"component"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(c.do(tok, http.MethodGet, "/components/"+name+"/capabilities", nil, http.StatusOK), &w); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	return w.Capabilities
}

// TestSeededStandardRolesAPI proves the ship-with example lands through the
// API: the meeting-room standard arrives with roles declared, so a system that
// conforms to it shows staffing work to do the moment it is created.
func TestSeededStandardRolesAPI(t *testing.T) {
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
	if mic := w.find(t, "room-mic"); !mic.FromStandard || mic.Quorum != 2 || mic.Understaffed != 2 {
		t.Fatalf("seeded room-mic = %+v, want inherited, quorum 2, two short", mic)
	}
	// The shipped catalog can actually satisfy what the shipped standard asks
	// for: a Samsung QM55 fills the display role without any hand-declaration.
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "qm-1", "product": "samsung-qm55"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPut, "/systems/seeded-room/roles/main-display/assignments/qm-1", nil, http.StatusNoContent)
}
