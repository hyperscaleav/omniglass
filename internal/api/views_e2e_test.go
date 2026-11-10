package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

type fleetViewWire struct {
	Locations []struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		LocationType string `json:"location_type"`
		Parent       string `json:"parent"`
		Verdict      string `json:"verdict"`
	} `json:"locations"`
	Systems []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Location string `json:"location"`
		Verdict  string `json:"verdict"`
		Dots     []struct {
			Component string `json:"component"`
			Name      string `json:"name"`
			Verdict   string `json:"verdict"`
			Primary   bool   `json:"primary"`
			Shared    bool   `json:"shared"`
		} `json:"dots"`
	} `json:"systems"`
}

// TestFleetViewAPI drives the fleet projection over HTTP as the canvas will.
// Two claims are worth driving end to end rather than at the gateway: that the
// wire carries dots and not component rows (a regression there is invisible on
// screen and costs a fleet-sized payload per paint), and that an alarm moves
// the dot, so the canvas is reading the same rollup the detail page reads
// rather than a second opinion.
func TestFleetViewAPI(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
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

	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "hq", "location_type": "campus"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "hq-r1", "location_type": "room", "parent": "hq"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/standards", map[string]any{"name": "hq-room", "label": "HQ Room"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPatch, "/standards/hq-room/roles/table-mic", map[string]any{
		"label": "Table Microphone", "quorum": 1,
		"accepted_types": []string{"video-bar"}, "impact": "outage",
	}, http.StatusOK)
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "hq-1", "standard_id": "hq-room", "location": "hq-r1"}, http.StatusCreated)
	bar := storagetest.MintProduct(t, ctx, gw, "video-bar")
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "bar-1", "product": bar.Name}, http.StatusCreated)
	c.do(ownerTok, http.MethodPut, "/systems/hq-1/roles/table-mic/assignments/bar-1", nil, http.StatusNoContent)

	// view reads the fleet and sets aside the boot seed's platform system
	// (#1625), which an all-scope caller sees beside the operator's systems.
	sawPlatform := false
	view := func(tok string) fleetViewWire {
		t.Helper()
		var w fleetViewWire
		if err := json.Unmarshal(c.do(tok, http.MethodGet, "/views/fleet", nil, http.StatusOK), &w); err != nil {
			t.Fatalf("decode fleet view: %v", err)
		}
		systems := w.Systems[:0]
		for _, s := range w.Systems {
			if s.Name == storage.PlatformSystemName {
				sawPlatform = true
				continue
			}
			systems = append(systems, s)
		}
		w.Systems = systems
		return w
	}

	v := view(ownerTok)
	if !sawPlatform {
		t.Error("the owner's fleet view lacks the platform system; an all-scope read sees it")
	}

	// The tree arrives flat, each node carrying its parent, because the client
	// assembles it and decides how the fleet is gathered into bands.
	var root, room struct {
		ID, Parent, Verdict string
	}
	for _, l := range v.Locations {
		switch l.Name {
		case "hq":
			root.ID, root.Parent, root.Verdict = l.ID, l.Parent, l.Verdict
			if l.LocationType != "campus" {
				t.Errorf("location type = %q, want campus: the band renders its chip from this", l.LocationType)
			}
		case "hq-r1":
			room.ID, room.Parent, room.Verdict = l.ID, l.Parent, l.Verdict
		}
	}
	if root.ID == "" || room.ID == "" {
		t.Fatalf("both locations must project, got %+v", v.Locations)
	}
	if root.Parent != "" {
		t.Error("a root location must carry no parent")
	}
	if room.Parent != root.ID {
		t.Errorf("room parent = %q, want the root's id %q", room.Parent, root.ID)
	}

	if len(v.Systems) != 1 || v.Systems[0].Name != "hq-1" {
		t.Fatalf("systems = %+v, want just hq-1", v.Systems)
	}
	sys := v.Systems[0]
	if sys.Location != room.ID {
		t.Errorf("system location = %q, want the room's id %q", sys.Location, room.ID)
	}
	if sys.Verdict != "healthy" {
		t.Errorf("staffed and quiet system verdict = %q, want healthy", sys.Verdict)
	}
	if len(sys.Dots) != 1 {
		t.Fatalf("dots = %+v, want one for bar-1", sys.Dots)
	}
	dot := sys.Dots[0]
	if dot.Component == "" || dot.Name != "bar-1" {
		t.Errorf("dot = %+v, want bar-1 with its component id", dot)
	}
	if !dot.Primary {
		t.Error("a component in its only system is primary there")
	}
	if dot.Shared {
		t.Error("a component in one system is not shared")
	}

	// Dots, not rows. The projection must not be quietly widened into a
	// component list: that is the whole reason it exists.
	raw := c.do(ownerTok, http.MethodGet, "/views/fleet", nil, http.StatusOK)
	var loose struct {
		Systems []struct {
			Dots []map[string]any `json:"dots"`
		} `json:"systems"`
	}
	if err := json.Unmarshal(raw, &loose); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	for _, s := range loose.Systems {
		for _, d := range s.Dots {
			for _, banned := range []string{"product", "tags", "properties", "effective_tags", "product_id", "location"} {
				if _, ok := d[banned]; ok {
					t.Errorf("a dot carries %q: the projection is a dot payload, not a component row", banned)
				}
			}
		}
	}

	// An alarm moves the dot and the verdicts above it, so the canvas reads the
	// same rollup the detail page does rather than a second opinion.
	c.do(ownerTok, http.MethodPost, "/components/bar-1/alarms", map[string]any{
		"severity": "high", "message": "no route to host",
	}, http.StatusCreated)

	v = view(ownerTok)
	if got := v.Systems[0].Dots[0].Verdict; got != "outage" {
		t.Errorf("dot verdict after a high alarm = %q, want outage", got)
	}
	if got := v.Systems[0].Verdict; got != "outage" {
		t.Errorf("system verdict after a high alarm = %q, want outage", got)
	}
	for _, l := range v.Locations {
		if l.Name == "hq" && l.Verdict != "outage" {
			t.Errorf("root verdict = %q, want outage: worst-wins climbs the tree", l.Verdict)
		}
	}
}

type platformViewWire struct {
	ReadAt      time.Time `json:"read_at"`
	Lab         bool      `json:"lab"`
	FiguresRead bool      `json:"figures_read"`
	System      *struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Verdict string `json:"verdict"`
	} `json:"system"`
	Servers []struct {
		Component string     `json:"component"`
		Name      string     `json:"name"`
		Version   string     `json:"version"`
		StartedAt *time.Time `json:"started_at"`
		Verdict   string     `json:"verdict"`
	} `json:"servers"`
	Groups []struct {
		Name  string `json:"name"`
		Label string `json:"label"`
		Keys  []struct {
			Name        string `json:"name"`
			Label       string `json:"label"`
			Unit        string `json:"unit"`
			Description string `json:"description"`
		} `json:"keys"`
	} `json:"groups"`
	Nodes []struct {
		Name      string `json:"name"`
		Component string `json:"component"`
		Verdict   string `json:"verdict"`
		Figures   map[string]struct {
			Value      float64   `json:"value"`
			ObservedAt time.Time `json:"observed_at"`
		} `json:"figures"`
	} `json:"nodes"`
}

// platformViewRig is a seeded platform with one node carrying a figure and a
// server carrying its boot identity, served by a lab handler.
type platformViewRig struct {
	ctx       context.Context
	dsn       string
	c         *apiClient
	owner     string
	startedAt time.Time
}

func newPlatformViewRig(t *testing.T) *platformViewRig {
	t.Helper()
	ctx := context.Background()
	dsn := seededDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	r := &platformViewRig{ctx: ctx, dsn: dsn, owner: bootstrapOwnerTok(t, ctx, gw)}
	// The narrow roles go in before the handler loads its role index.
	r.role(t, "node-reader", "node:read")
	r.role(t, "placed-reader", "component:read", "system:read")
	r.role(t, "component-reader", "component:read")
	srv := httptest.NewServer(api.NewHandler(gw, api.WithLab(true)))
	t.Cleanup(srv.Close)
	r.c = &apiClient{t: t, ctx: ctx, base: srv.URL}

	r.c.do(r.owner, http.MethodPost, "/nodes", map[string]any{"name": "edge-alpha"}, http.StatusCreated)
	nodes, err := gw.ListNodes(ctx, scope.Set{All: true})
	if err != nil || len(nodes) != 1 || nodes[0].PlatformComponentID == nil {
		t.Fatalf("nodes = %+v, %v; want edge-alpha with its platform component", nodes, err)
	}
	if err := gw.RecordPlatformMetrics(ctx, []storage.PlatformMetric{
		{ComponentID: *nodes[0].PlatformComponentID, Key: writerclass.CheckinAge, Value: 7, TS: time.Now().UTC()},
	}); err != nil {
		t.Fatalf("record a checkin age: %v", err)
	}
	server, err := gw.EnsureServerComponent(ctx, "lab-server")
	if err != nil {
		t.Fatalf("ensure server component: %v", err)
	}
	r.startedAt = time.Date(2026, 11, 9, 9, 30, 0, 0, time.UTC)
	if err := gw.RecordServerIdentity(ctx, server, "v9.9.9", r.startedAt); err != nil {
		t.Fatalf("record server identity: %v", err)
	}
	return r
}

func (r *platformViewRig) read(t *testing.T, tok string) platformViewWire {
	t.Helper()
	var w platformViewWire
	if err := json.Unmarshal(r.c.do(tok, http.MethodGet, "/views/platform", nil, http.StatusOK), &w); err != nil {
		t.Fatalf("decode platform view: %v", err)
	}
	return w
}

// location creates a root campus and returns its id, the scope a narrow grant
// names.
func (r *platformViewRig) location(t *testing.T, name string) string {
	t.Helper()
	var loc struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(r.c.do(r.owner, http.MethodPost, "/locations", map[string]any{"name": name, "location_type": "campus"}, http.StatusCreated), &loc); err != nil || loc.ID == "" {
		t.Fatalf("create location %s: %v", name, err)
	}
	return loc.ID
}

// role inserts an operator-defined role carrying exactly perms. The handler
// reads roles once at construction, so a role goes in before it.
func (r *platformViewRig) role(t *testing.T, name string, perms ...string) {
	t.Helper()
	conn, err := pgx.Connect(r.ctx, r.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(r.ctx)
	if _, err := conn.Exec(r.ctx, `insert into role (name, official, permissions, inherits) values ($1, false, $2, '{}')`, name, perms); err != nil {
		t.Fatalf("insert role %s: %v", name, err)
	}
}

// TestPlatformViewAPIServesTheSeededViewer: the seeded Viewer role, an
// all-scope reader, gets the whole page in one call: the platform system, the
// server's build and start time, the column groups with their catalog facts,
// and the node's figure. A key the node never reported is absent.
func TestPlatformViewAPIServesTheSeededViewer(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	r := newPlatformViewRig(t)
	before := time.Now().UTC()
	w := r.read(t, setupAllViewer(t, r.ctx, r.dsn, "platform-viewer"))

	if w.ReadAt.Before(before.Add(-time.Second)) {
		t.Errorf("read_at = %s, want the server's clock at the read", w.ReadAt)
	}
	if !w.Lab {
		t.Error("lab = false from a lab handler")
	}
	if !w.FiguresRead {
		t.Error("figures_read = false for an all-scope viewer")
	}
	if w.System == nil || w.System.Name != storage.PlatformSystemName || w.System.Verdict == "" {
		t.Errorf("system = %+v, want the platform system with its verdict", w.System)
	}
	if len(w.Servers) != 1 || w.Servers[0].Version != "v9.9.9" ||
		w.Servers[0].StartedAt == nil || !w.Servers[0].StartedAt.Equal(r.startedAt) {
		t.Errorf("servers = %+v, want lab-server on v9.9.9 started %s", w.Servers, r.startedAt)
	}
	if len(w.Groups) != len(storage.PlatformViewGroups) {
		t.Fatalf("groups = %+v, want %d", w.Groups, len(storage.PlatformViewGroups))
	}
	checkin := w.Groups[0]
	if checkin.Name != "checkin" || len(checkin.Keys) != 1 || checkin.Keys[0].Name != writerclass.CheckinAge ||
		checkin.Keys[0].Label == "" || checkin.Keys[0].Description == "" {
		t.Errorf("checkin group = %+v, want checkin-age with its catalog label and description", checkin)
	}
	if len(w.Nodes) != 1 || w.Nodes[0].Name != "edge-alpha" || w.Nodes[0].Component == "" {
		t.Fatalf("nodes = %+v, want edge-alpha with its component", w.Nodes)
	}
	n := w.Nodes[0]
	if got, ok := n.Figures[writerclass.CheckinAge]; !ok || got.Value != 7 {
		t.Errorf("edge-alpha checkin-age = %+v (present %v), want 7", got, ok)
	}
	if _, ok := n.Figures[writerclass.ScheduleLateness]; ok {
		t.Error("edge-alpha carries a schedule-lateness it never reported: a missing figure is absent, not zero")
	}
	if n.Verdict == "" {
		t.Error("edge-alpha verdict missing for an all-scope component read")
	}
}

// TestPlatformViewAPIOmitsWhatTheCallerCannotRead: a fleet-wide node:read
// holder whose component and system reads are location-scoped sees every row,
// but no figure, verdict, server or system: the platform components are
// unplaced, so a scoped read never reaches them. figures_read says why the
// cells are empty.
func TestPlatformViewAPIOmitsWhatTheCallerCannotRead(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	r := newPlatformViewRig(t)
	hq := r.location(t, "hq")
	tok := principalWithGrants(t, r.ctx, r.dsn, "fleet-node-reader", []grant{
		{role: "node-reader", scopeKind: "all"},
		{role: "placed-reader", scopeKind: "location", scopeID: hq},
	})
	w := r.read(t, tok)
	if w.FiguresRead {
		t.Error("figures_read = true for a location-scoped component read")
	}
	if w.System != nil {
		t.Errorf("system = %+v, want it absent for a location-scoped system read", w.System)
	}
	if len(w.Servers) != 0 {
		t.Errorf("servers = %+v, want none", w.Servers)
	}
	if len(w.Nodes) != 1 || w.Nodes[0].Name != "edge-alpha" {
		t.Fatalf("nodes = %+v, want the edge-alpha row", w.Nodes)
	}
	if w.Nodes[0].Figures != nil || w.Nodes[0].Verdict != "" {
		t.Errorf("edge-alpha figures = %v, verdict = %q, want both omitted", w.Nodes[0].Figures, w.Nodes[0].Verdict)
	}
	if len(w.Groups) == 0 {
		t.Error("groups missing: the columns are the catalog's, readable without a component read")
	}
}

// TestPlatformViewAPIRefusesWithoutAFleetWideNodeRead: no node:read is a 403
// at the gate, and a location-scoped node:read is a 403 from the read, never
// an empty table that reads as "no nodes".
func TestPlatformViewAPIRefusesWithoutAFleetWideNodeRead(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	r := newPlatformViewRig(t)
	hq := r.location(t, "hq")
	none := principalWithGrants(t, r.ctx, r.dsn, "no-node-read", []grant{{role: "component-reader", scopeKind: "all"}})
	r.c.do(none, http.MethodGet, "/views/platform", nil, http.StatusForbidden)

	located := principalWithGrants(t, r.ctx, r.dsn, "hq-viewer", []grant{{role: "viewer", scopeKind: "location", scopeID: hq}})
	r.c.do(located, http.MethodGet, "/views/platform", nil, http.StatusForbidden)
}

// TestPlatformViewIsStampedNodeRead: the generated reference names the
// page's door, so a reader searching it for node:read finds this read.
func TestPlatformViewIsStampedNodeRead(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../api/openapi.json")
	if err != nil {
		t.Fatalf("read openapi.json (run make gen): %v", err)
	}
	var spec struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
			Permission  string `json:"x-omniglass-permission"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse openapi.json: %v", err)
	}
	op, ok := spec.Paths["/views/platform"]["get"]
	if !ok {
		t.Fatal("GET /views/platform is not in the spec")
	}
	if op.OperationID != "get-platform-view" || op.Permission != "node:read" {
		t.Errorf("GET /views/platform = %+v, want get-platform-view stamped node:read", op)
	}
}
