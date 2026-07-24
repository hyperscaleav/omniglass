package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

type alarmWire struct {
	ID           string   `json:"id"`
	Component    string   `json:"component"`
	Severity     string   `json:"severity"`
	Message      string   `json:"message"`
	Capabilities []string `json:"capabilities"`
	Active       bool     `json:"active"`
}

type healthWire struct {
	OwnerKind string `json:"owner_kind"`
	Owner     string `json:"owner"`
	Verdict   string `json:"verdict"`
	Roles     []struct {
		Name       string      `json:"name"`
		Impact     string      `json:"impact"`
		Quorum     int         `json:"quorum"`
		Satisfying int         `json:"satisfying"`
		Impaired   bool        `json:"impaired"`
		Degraded   []string    `json:"degraded"`
		Alarms     []alarmWire `json:"alarms"`
	} `json:"roles"`
	Systems []struct {
		Name    string `json:"name"`
		Verdict string `json:"verdict"`
	} `json:"systems"`
	Transitions []struct {
		Verdict string `json:"verdict"`
	} `json:"transitions"`
}

// TestHealthAPI drives the alarm and health surfaces over HTTP as an operator
// would: raise an alarm on a component, watch its system and the location above
// it go degraded, read WHY (the impaired role, the capability it lost, the alarm
// that took it), then clear the alarm and watch everything come back. The
// drill-down from a degraded location to the causing alarm is the point of the
// slice, so it is driven end to end here rather than asserted at the gateway.
// Skipped under -short.
func TestHealthAPI(t *testing.T) {
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

	// hq (campus) > hq-r1 (room), a system in the room conforming to a standard
	// that wants one table mic, and a room bar staffing it.
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{
		"name": "hq", "location_type": "campus",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{
		"name": "hq-r1", "location_type": "room", "parent": "hq",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/standards", map[string]any{
		"name": "hq-room", "display_name": "HQ Room",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodPut, "/standards/hq-room/roles/table-mic", map[string]any{
		"display_name": "Table Microphone", "quorum": 1,
		"capabilities": []string{"microphone", "speaker"}, "impact": "outage",
	}, http.StatusOK)
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{
		"name": "hq-1", "standard_id": "hq-room", "location": "hq-r1",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{
		"name": "bar-1", "product": "cisco-room-bar",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodPut, "/systems/hq-1/roles/table-mic/assignments/bar-1", nil, http.StatusNoContent)

	health := func(tok, path string) healthWire {
		t.Helper()
		var w healthWire
		if err := json.Unmarshal(c.do(tok, http.MethodGet, path, nil, http.StatusOK), &w); err != nil {
			t.Fatalf("decode health %s: %v", path, err)
		}
		return w
	}

	// Staffed and quiet: healthy, and the impact the standard declared rides along
	// on the role even while nothing is wrong.
	sys := health(ownerTok, "/systems/hq-1/health")
	if sys.Verdict != "healthy" {
		t.Fatalf("staffed system verdict = %q, want healthy", sys.Verdict)
	}
	if len(sys.Roles) != 1 || sys.Roles[0].Impact != "outage" || sys.Roles[0].Impaired {
		t.Fatalf("roles = %+v, want one table-mic at impact outage, not impaired", sys.Roles)
	}

	// Raise an alarm that takes away microphone, which the role requires.
	var raised alarmWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/components/bar-1/alarms", map[string]any{
		"severity": "critical", "message": "mic array not responding",
		"capabilities": []string{"microphone"},
	}, http.StatusCreated), &raised); err != nil {
		t.Fatalf("decode raised alarm: %v", err)
	}
	if !raised.Active || raised.Severity != "critical" {
		t.Fatalf("raised alarm = %+v, want an active critical", raised)
	}

	// The role's impact is outage, so the system is an outage even though the
	// component only lost one capability. What the slot meant is what decides.
	sys = health(ownerTok, "/systems/hq-1/health")
	if sys.Verdict != "outage" {
		t.Fatalf("system verdict = %q, want outage (the impaired role's declared impact)", sys.Verdict)
	}
	role := sys.Roles[0]
	if !role.Impaired || role.Satisfying != 0 {
		t.Fatalf("role = %+v, want impaired with nobody satisfying it", role)
	}
	if len(role.Degraded) != 1 || role.Degraded[0] != "microphone" {
		t.Fatalf("degraded = %v, want the required capability the alarm took", role.Degraded)
	}
	if len(role.Alarms) != 1 || role.Alarms[0].ID != raised.ID {
		t.Fatalf("alarms = %+v, want the causing alarm named", role.Alarms)
	}

	// The location rolls up and names the system to drill into.
	loc := health(ownerTok, "/locations/hq/health")
	if loc.Verdict != "outage" {
		t.Fatalf("location verdict = %q, want outage", loc.Verdict)
	}
	if len(loc.Systems) != 1 || loc.Systems[0].Name != "hq-1" || loc.Systems[0].Verdict != "outage" {
		t.Fatalf("location systems = %+v, want hq-1 in outage", loc.Systems)
	}

	// The alarm list is the active set by default and the history with the flag.
	var listed struct {
		Alarms []alarmWire `json:"alarms"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/bar-1/alarms", nil, http.StatusOK), &listed); err != nil {
		t.Fatalf("decode alarms: %v", err)
	}
	if len(listed.Alarms) != 1 || listed.Alarms[0].ID != raised.ID {
		t.Fatalf("alarms = %+v, want the one raised", listed.Alarms)
	}

	// Clear it: everything comes back, and the cleared row survives in the history.
	c.do(ownerTok, http.MethodDelete, "/components/bar-1/alarms/"+raised.ID, nil, http.StatusNoContent)
	if sys = health(ownerTok, "/systems/hq-1/health"); sys.Verdict != "healthy" {
		t.Fatalf("system after clearing = %q, want healthy", sys.Verdict)
	}
	if loc = health(ownerTok, "/locations/hq/health"); loc.Verdict != "healthy" {
		t.Fatalf("location after clearing = %q, want healthy", loc.Verdict)
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/bar-1/alarms", nil, http.StatusOK), &listed); err != nil {
		t.Fatalf("decode active alarms: %v", err)
	}
	if len(listed.Alarms) != 0 {
		t.Fatalf("active alarms after clearing = %+v, want none", listed.Alarms)
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/bar-1/alarms?include_cleared=true", nil, http.StatusOK), &listed); err != nil {
		t.Fatalf("decode alarm history: %v", err)
	}
	if len(listed.Alarms) != 1 || listed.Alarms[0].Active {
		t.Fatalf("alarm history = %+v, want the cleared row kept", listed.Alarms)
	}

	// The transition series is edges and only edges: outage the moment the system
	// was created (it inherited a role the standard already declared and nobody was
	// filling it), healthy when the bar was assigned, outage on the alarm, healthy
	// on the clear. Four writes moved the verdict; the reads in between added
	// nothing, which is the whole point of recomputing at the write rather than on
	// the page view.
	sys = health(ownerTok, "/systems/hq-1/health")
	want := []string{"outage", "healthy", "outage", "healthy"}
	if len(sys.Transitions) != len(want) {
		t.Fatalf("transitions = %+v, want exactly %d edges, no samples", sys.Transitions, len(want))
	}
	for i, w := range want {
		if sys.Transitions[i].Verdict != w {
			t.Fatalf("transitions = %+v, want %v", sys.Transitions, want)
		}
	}

	// Request faults are named, not 500s.
	c.do(ownerTok, http.MethodPost, "/components/bar-1/alarms", map[string]any{
		"severity": "critical", "capabilities": []string{"telepathy"},
	}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPost, "/components/no-such/alarms", map[string]any{
		"severity": "info",
	}, http.StatusNotFound)
	c.do(ownerTok, http.MethodDelete, "/components/bar-1/alarms/"+raised.ID, nil, http.StatusNotFound)
	c.do(ownerTok, http.MethodDelete, "/components/bar-1/alarms/not-a-uuid", nil, http.StatusNotFound)
}

// TestHealthAPIScope proves the health and alarm routes are gated and
// scope-injected like every other surface: a viewer scoped elsewhere gets a
// non-disclosing 404 rather than a forbidden that confirms the thing exists, and
// cannot raise an alarm at all.
func TestHealthAPIScope(t *testing.T) {
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

	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{
		"name": "north", "location_type": "campus",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{
		"name": "south", "location_type": "campus",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{
		"name": "north-sys", "location": "north",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{
		"name": "south-sys", "location": "south",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "north-bar"}, http.StatusCreated)

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var northID, northSysID string
	if err := conn.QueryRow(ctx, `select id from location where name = 'north'`).Scan(&northID); err != nil {
		t.Fatalf("north id: %v", err)
	}
	if err := conn.QueryRow(ctx, `select id from system where name = 'north-sys'`).Scan(&northSysID); err != nil {
		t.Fatalf("north-sys id: %v", err)
	}

	// A viewer that can see only the north campus: the *:read floor passes the
	// permission gate, scope injection is what hides the south.
	viewerTok := setupScopedPrincipal(t, ctx, dsn, "viewer-north-health",
		grantSpec{role: "viewer", scopeKind: "location", scopeID: northID},
		grantSpec{role: "viewer", scopeKind: "system", scopeID: northSysID})
	c.do(viewerTok, http.MethodGet, "/locations/north/health", nil, http.StatusOK)
	c.do(viewerTok, http.MethodGet, "/locations/south/health", nil, http.StatusNotFound)
	c.do(viewerTok, http.MethodGet, "/systems/north-sys/health", nil, http.StatusOK)
	c.do(viewerTok, http.MethodGet, "/systems/south-sys/health", nil, http.StatusNotFound)

	// Raising an alarm is a component:update, which a viewer does not carry: the
	// capability fast-reject is a 403 before scope is even consulted.
	c.do(viewerTok, http.MethodPost, "/components/north-bar/alarms", map[string]any{
		"severity": "info",
	}, http.StatusForbidden)
}
