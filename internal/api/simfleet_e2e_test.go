package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/simhub"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// fakeHub is the server's line to its simulators, with a recorded fire.
type fakeHub struct {
	*simhub.Registry
	mu    sync.Mutex
	fired []struct {
		outpost string
		op      collection.SimOperation
	}
	fireErr error
	// observation answers EndpointObservation by outpost; an outpost absent from it
	// has no simulator. observeErr, when set, answers every outpost.
	observation map[string]collection.EndpointObservation
	observeErr  error
}

func (h *fakeHub) EndpointObservation(_ context.Context, outpost string) (collection.EndpointObservation, error) {
	if h.observeErr != nil {
		return collection.EndpointObservation{}, h.observeErr
	}
	o, ok := h.observation[outpost]
	if !ok {
		return o, collection.ErrNoSimulator
	}
	return o, nil
}

func (h *fakeHub) SimStatus(outpost string) (simhub.Status, bool) { return h.Status(outpost) }

func (h *fakeHub) FireSimOperation(_ context.Context, outpost string, op collection.SimOperation) (collection.SimOperationResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fired = append(h.fired, struct {
		outpost string
		op      collection.SimOperation
	}{outpost, op})
	if h.fireErr != nil {
		return collection.SimOperationResult{}, h.fireErr
	}
	return collection.SimOperationResult{Body: "INPUT hdmi2", TookMs: 1}, nil
}

type fleetBody struct {
	Connected bool `json:"connected"`
	Outposts  []struct {
		Outpost    string `json:"outpost"`
		Connected  bool   `json:"connected"`
		Components []struct {
			Component string `json:"component"`
			Address   string `json:"address"`
		} `json:"components"`
	} `json:"outposts"`
}

type componentSimBody struct {
	Simulated bool   `json:"simulated"`
	Outpost   string `json:"outpost"`
	Product   string `json:"product"`
	Profile   string `json:"profile"`
	Address   string `json:"address"`
	ServedBy  string `json:"served_by"`
	Activity  *struct {
		Component string `json:"component"`
	} `json:"activity"`
	Surface []struct {
		Name     string `json:"name"`
		Protocol string `json:"protocol"`
		Port     int    `json:"port"`
		Routes   []struct {
			Method string `json:"method"`
			Path   string `json:"path"`
		} `json:"routes"`
		Commands []struct {
			Match string `json:"match"`
		} `json:"commands"`
		OIDs []struct {
			OID  string `json:"oid"`
			Path string `json:"path"`
		} `json:"oids"`
	} `json:"surface"`
	Variants []string `json:"variants"`
}

// TestSimulationFleetAPI drives the console's simulation surfaces end to
// end: the fleet of simulators and their devices, the profile catalog, one
// component's simulation with its API surface, firing an operation at it,
// and adding a simulated device from a profile. Reads are simulation:read;
// fire and add are simulation:update.
func TestSimulationFleetAPI(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	for _, n := range []string{"site-a", "site-b"} {
		if _, err := gw.CreateOutpost(ctx, "", storage.OutpostSpec{Name: n}, all, all); err != nil {
			t.Fatal(err)
		}
	}
	panel, prof := lineProtocolProduct(t, ctx, gw)
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-1", ProductName: &panel}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "loose"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	comp, outpost := "panel-1", "site-a"
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "tcp", Component: &comp, Outpost: &outpost,
		Params: []byte(`{"target":"127.60.0.1:51325"}`)}, all); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var panelID string
	if err := conn.QueryRow(ctx, `select id from component where name = 'panel-1'`).Scan(&panelID); err != nil {
		t.Fatal(err)
	}

	if _, err := conn.Exec(ctx, `insert into role (name, permissions) values ('sim-scoped-reader', '{"simulation:read","component:read"}')`); err != nil {
		t.Fatal(err)
	}
	hub := &fakeHub{Registry: simhub.New()}
	srv := httptest.NewServer(api.NewHandler(gw, api.WithSimHub(hub)))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	viewer := setupAllViewer(t, ctx, dsn, "simfleet-viewer")
	get := func(tok, path string, into any) {
		t.Helper()
		if err := json.Unmarshal(c.do(tok, http.MethodGet, path, nil, http.StatusOK), into); err != nil {
			t.Fatal(err)
		}
	}

	// No simulator has pulled: the fleet reads as disconnected and empty,
	// which is what hides the console's Simulation page.
	var fleet fleetBody
	get(ownerTok, "/simulation", &fleet)
	if fleet.Connected || len(fleet.Outposts) != 0 {
		t.Fatalf("before any simulator: %+v", fleet)
	}

	hub.Pulled("site-a", collection.SimCapabilities{PortOffset: 10000}, []string{panelID})
	get(viewer, "/simulation", &fleet)
	if !fleet.Connected || len(fleet.Outposts) != 1 || fleet.Outposts[0].Outpost != "site-a" || !fleet.Outposts[0].Connected ||
		len(fleet.Outposts[0].Components) != 1 || fleet.Outposts[0].Components[0].Component != "panel-1" {
		t.Fatalf("fleet = %+v, want site-a's simulator serving panel-1", fleet)
	}
	c.do("", http.MethodGet, "/simulation", nil, http.StatusUnauthorized)

	// simulation:read is the whole gate (no outpost read needed): a caller
	// scoped to one component sees the outpost that simulates it, with only
	// that component; a caller whose scope holds no simulated component
	// sees no simulator at all, so the console hides its page.
	scopedTo := func(component string) string {
		tok, hash, prefix, err := auth.NewBearerToken()
		if err != nil {
			t.Fatal(err)
		}
		var pid string
		if err := conn.QueryRow(ctx, `insert into users (kind) values ('machine') returning id`).Scan(&pid); err != nil {
			t.Fatal(err)
		}
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`insert into machine (user_id, name) values ($1, $2)`, []any{pid, "simfleet-scoped-" + component}},
			{`insert into credential (user_id, kind, secret_hash, prefix) values ($1, 'bearer', $2, $3)`, []any{pid, hash, prefix}},
			{`insert into user_grant (user_id, role_id, scope_kind, scope_id) values ($1, (select id from role where name = 'sim-scoped-reader'), 'component', (select id::text from component where name = $2))`, []any{pid, component}},
		} {
			if _, err := conn.Exec(ctx, q.sql, q.args...); err != nil {
				t.Fatalf("%s: %v", q.sql, err)
			}
		}
		return tok
	}
	var scoped fleetBody
	get(scopedTo("panel-1"), "/simulation", &scoped)
	if !scoped.Connected || len(scoped.Outposts) != 1 || scoped.Outposts[0].Outpost != "site-a" ||
		len(scoped.Outposts[0].Components) != 1 || scoped.Outposts[0].Components[0].Component != "panel-1" {
		t.Fatalf("a caller scoped to panel-1 reads %+v, want site-a with panel-1", scoped)
	}
	var elsewhere fleetBody
	get(scopedTo("loose"), "/simulation", &elsewhere)
	if elsewhere.Connected || len(elsewhere.Outposts) != 0 {
		t.Fatalf("a caller scoped to no simulated component reads %+v, want no simulator", elsewhere)
	}

	// The catalog lists each profile with the products it simulates.
	var cat struct {
		Profiles []struct {
			Name       string   `json:"name"`
			Products   []string `json:"products"`
			DeviceType string   `json:"device_type"`
		} `json:"profiles"`
	}
	get(viewer, "/simulation/profiles", &cat)
	found := false
	for _, p := range cat.Profiles {
		if p.Name == prof.Name {
			found = len(p.Products) > 0 && p.Products[0] == prof.Name
		}
	}
	if !found {
		t.Fatalf("catalog = %+v, want %s simulating its own product first", cat, prof.Name)
	}

	// A simulated component's surface: every interface its profile serves,
	// with its routes, commands and OIDs, ports as the simulator binds them.
	var sim componentSimBody
	get(viewer, "/components/panel-1/simulation", &sim)
	if !sim.Simulated || sim.Outpost != "site-a" || sim.Product != panel || sim.Profile != prof.Name || sim.Address != "127.60.0.1" {
		t.Fatalf("panel-1 simulation = %+v", sim)
	}
	byName := map[string]int{}
	for i, s := range sim.Surface {
		byName[s.Name] = i
	}
	// Every surface the profile declares is listed, its port lifted as the
	// simulator lifts it, with its routes, commands and OIDs.
	lift := func(port int) int {
		if port > 0 && port < 1024 {
			return port + 10000
		}
		return port
	}
	for _, want := range prof.Interfaces {
		i, ok := byName[want.Name]
		if !ok {
			t.Fatalf("surface %s missing from %+v", want.Name, sim.Surface)
		}
		got := sim.Surface[i]
		if got.Port != lift(want.Port) || len(got.Routes) != len(want.Routes) || len(got.Commands) != len(want.Commands) || len(got.OIDs) != len(want.OIDMap) {
			t.Fatalf("surface %s = %+v, want port %d and the profile's routes, commands and OIDs", want.Name, got, lift(want.Port))
		}
		if len(want.Commands) > 0 && got.Commands[0].Match != want.Commands[0].Match {
			t.Fatalf("surface %s commands = %+v, in the profile's order", want.Name, got.Commands)
		}
	}
	lineSurface := ""
	for _, i := range prof.Interfaces {
		if i.Protocol == "tcp" && len(i.Commands) > 0 && lineSurface == "" {
			lineSurface = i.Name
		}
	}
	var loose componentSimBody
	get(viewer, "/components/loose/simulation", &loose)
	if loose.Simulated {
		t.Fatalf("an unplaced component reads simulated: %+v", loose)
	}

	// Fire: the operation reaches site-a's simulator for panel-1, with the
	// kind taken from the interface, and the device's answer comes back.
	var fired struct {
		Body   string `json:"body"`
		TookMs int64  `json:"took_ms"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/components/panel-1/simulation:fire",
		map[string]any{"surface": lineSurface, "command": "SET INPUT hdmi2"}, http.StatusOK), &fired); err != nil {
		t.Fatal(err)
	}
	if fired.Body != "INPUT hdmi2" || len(hub.fired) != 1 || hub.fired[0].outpost != "site-a" ||
		hub.fired[0].op.ComponentID != panelID || hub.fired[0].op.Kind != "command" || hub.fired[0].op.Protocol != "tcp" {
		t.Fatalf("fired %+v, answer %+v", hub.fired, fired)
	}
	var audits int
	if err := conn.QueryRow(ctx, `select count(*) from audit_log where resource = 'sim_operation' and resource_id = $1`, panelID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("%d audit rows for one fire", audits)
	}
	c.do(viewer, http.MethodPost, "/components/panel-1/simulation:fire", map[string]any{"surface": lineSurface, "command": "GET INPUT"}, http.StatusForbidden)
	c.do(ownerTok, http.MethodPost, "/components/panel-1/simulation:fire", map[string]any{"surface": "nope", "command": "GET INPUT"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPost, "/components/loose/simulation:fire", map[string]any{"surface": lineSurface, "command": "GET INPUT"}, http.StatusConflict)
	hub.fireErr = collection.ErrNoSimulator
	c.do(ownerTok, http.MethodPost, "/components/panel-1/simulation:fire", map[string]any{"surface": lineSurface, "command": "GET INPUT"}, http.StatusServiceUnavailable)

	// Plan a simulated device before adding it: the address and endpoints the
	// add would create, read without creating anything.
	type plannedEndpoint struct {
		Driver    string            `json:"driver"`
		Inputs    map[string]string `json:"inputs"`
		Transport string            `json:"transport"`
		Target    string            `json:"target"`
	}
	var planned struct {
		Outpost   string            `json:"outpost"`
		Product   string            `json:"product"`
		Address   string            `json:"address"`
		Endpoints []plannedEndpoint `json:"endpoints"`
	}
	countComponents := func() int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, `select count(*) from component`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := countComponents()
	get(viewer, "/outposts/site-a/simulation/components:plan?product="+panel, &planned)
	if planned.Outpost != "site-a" || planned.Product != panel || !strings.HasPrefix(planned.Address, "127.60.") ||
		len(planned.Endpoints) != 1 || planned.Endpoints[0].Driver != "newtron-panel-nvp" || len(planned.Endpoints[0].Inputs) == 0 {
		t.Fatalf("plan = %+v, want a newtron-panel-nvp attach at a free address in 127.60.0.0/16 on site-a", planned)
	}
	if got := countComponents(); got != before {
		t.Fatalf("the plan changed the component count from %d to %d, want it to create nothing", before, got)
	}
	planOut := c.do(viewer, http.MethodGet, "/outposts/site-a/simulation/components:plan?product="+storagetest.MintProduct(t, ctx, gw, "").Name, nil, http.StatusUnprocessableEntity)
	if !strings.Contains(string(planOut), "simulat") {
		t.Fatalf("an unsimulated product's plan was refused with %s, want it to say why", planOut)
	}
	c.do(viewer, http.MethodGet, "/outposts/site-b/simulation/components:plan?product="+panel, nil, http.StatusConflict)
	c.do(viewer, http.MethodGet, "/outposts/nowhere/simulation/components:plan?product="+panel, nil, http.StatusNotFound)
	// simulation:read is the gate: an all-scope reader without it is refused.
	if _, err := conn.Exec(ctx, `insert into role (name, permissions) values ('sim-blind-reader', '{"component:read","outpost:read"}')`); err != nil {
		t.Fatal(err)
	}
	blindTok, blindHash, blindPrefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	var blindID string
	if err := conn.QueryRow(ctx, `insert into users (kind) values ('machine') returning id`).Scan(&blindID); err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`insert into machine (user_id, name) values ($1, 'simfleet-blind')`, []any{blindID}},
		{`insert into credential (user_id, kind, secret_hash, prefix) values ($1, 'bearer', $2, $3)`, []any{blindID, blindHash, blindPrefix}},
		{`insert into user_grant (user_id, role_id, scope_kind, scope_id) values ($1, (select id from role where name = 'sim-blind-reader'), 'all', null)`, []any{blindID}},
	} {
		if _, err := conn.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
	}
	c.do(blindTok, http.MethodGet, "/outposts/site-a/simulation/components:plan?product="+panel, nil, http.StatusForbidden)

	// Add a simulated device from a profile: a component of the product,
	// at a free address in the simulated block, with the endpoint the
	// planner picks (the NVP attach, no secret needed), on the outpost.
	var added struct {
		Component string `json:"component"`
		Address   string `json:"address"`
		Endpoints int    `json:"endpoints"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/outposts/site-a/simulation/components",
		map[string]any{"name": "panel-9", "product": panel}, http.StatusCreated), &added); err != nil {
		t.Fatal(err)
	}
	if added.Component != "panel-9" || added.Address != "127.60.0.2" || added.Endpoints != 1 {
		t.Fatalf("added = %+v, want panel-9 at the free 127.60.0.2 with one endpoint", added)
	}
	var driver, onOutpost string
	if err := conn.QueryRow(ctx, `select coalesce(d.name, ''), n.name from endpoint e join outpost n on n.user_id = e.outpost_id
		left join driver d on d.id = e.driver_id where e.component = (select id from component where name = 'panel-9')`).Scan(&driver, &onOutpost); err != nil {
		t.Fatal(err)
	}
	if driver != "newtron-panel-nvp" || onOutpost != "site-a" {
		t.Fatalf("panel-9's endpoint is %q on %q, want a newtron-panel-nvp attach on site-a", driver, onOutpost)
	}
	// The plan read before the add is what the add created: its address, its
	// driver, and every input it sent (the stored set has the driver's
	// defaults baked in beside them).
	var storedInputs map[string]string
	if err := conn.QueryRow(ctx, `select inputs from endpoint where component = (select id from component where name = 'panel-9')`).Scan(&storedInputs); err != nil {
		t.Fatal(err)
	}
	if planned.Address != added.Address || planned.Endpoints[0].Driver != driver {
		t.Fatalf("plan %+v, but the add created %s on %s", planned, driver, added.Address)
	}
	for k, v := range planned.Endpoints[0].Inputs {
		if storedInputs[k] != v {
			t.Fatalf("planned input %s=%q, the add stored %q (all: %v)", k, v, storedInputs[k], storedInputs)
		}
	}
	var withNew simulationBody
	get(ownerTok, "/outposts/site-a/simulation", &withNew)
	if len(withNew.Devices) != 2 {
		t.Fatalf("site-a lists %d devices after the add, want 2", len(withNew.Devices))
	}
	c.do(viewer, http.MethodPost, "/outposts/site-a/simulation/components", map[string]any{"name": "panel-10", "product": panel}, http.StatusForbidden)
	// A product no profile simulates (a minted one) is refused, naming why.
	unsimulated := storagetest.MintProduct(t, ctx, gw, "")
	out := c.do(ownerTok, http.MethodPost, "/outposts/site-a/simulation/components", map[string]any{"name": "panel-10", "product": unsimulated.Name}, http.StatusUnprocessableEntity)
	if !strings.Contains(string(out), "simulat") {
		t.Fatalf("an unsimulated product was refused with %s, want it to say why", out)
	}
	c.do(ownerTok, http.MethodPost, "/outposts/site-b/simulation/components", map[string]any{"name": "panel-11", "product": panel}, http.StatusConflict)

	// A component name repeats across places: the add wires the component it
	// created, never a namesake elsewhere.
	for _, l := range []string{"loc-a", "loc-b"} {
		if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: l, LocationType: "campus"}, all, all); err != nil {
			t.Fatal(err)
		}
	}
	locA := "loc-a"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "dup", ProductName: &panel, LocationName: &locA}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	c.do(ownerTok, http.MethodPost, "/outposts/site-a/simulation/components", map[string]any{"name": "dup", "product": panel, "location": "loc-b"}, http.StatusCreated)
	var dupEndpoints map[string]int = map[string]int{}
	rows, err := conn.Query(ctx, `select l.name, count(e.id) from component c join location l on l.id = c.location_id
		left join endpoint e on e.component = c.id where c.name = 'dup' group by l.name`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var l string
		var n int
		if err := rows.Scan(&l, &n); err != nil {
			t.Fatal(err)
		}
		dupEndpoints[l] = n
	}
	rows.Close()
	if dupEndpoints["loc-a"] != 0 || dupEndpoints["loc-b"] != 1 {
		t.Fatalf("endpoints per dup = %v, want the new one at loc-b wired and loc-a's untouched", dupEndpoints)
	}

	// A method that is no HTTP method is refused at the door.
	c.do(ownerTok, http.MethodPost, "/components/panel-1/simulation:fire", map[string]any{"surface": "http-api", "method": "GE T", "path": "/"}, http.StatusUnprocessableEntity)
}

// lineProtocolProduct is a seeded product on the newtron-panel-nvp driver that a
// simulator profile simulates as its own product (a panel speaking the
// line protocol), read from the catalog rather than named, so a catalog
// rename does not break this test.
func lineProtocolProduct(t *testing.T, ctx context.Context, gw *storage.PG) (string, *profile.DeviceProfile) {
	t.Helper()
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	products, err := gw.ListProducts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range products {
		if p.DriverName == nil || *p.DriverName != "newtron-panel-nvp" {
			continue
		}
		if d, ok := store.Devices[p.Name]; ok {
			return p.Name, d
		}
	}
	t.Fatal("no seeded newtron-panel-nvp product is a simulator profile's own product")
	return "", nil
}

// TestComponentAimedAtASimulatedDeviceIsServedByIt holds #2461: a hand-built
// component whose endpoint targets the address a simulated device already
// answers on is served by that simulation. The outpost lists one device (not a
// second, failed one with the wrong profile), the simulator is handed one, and
// the component's own simulation read is the device's, naming it.
func TestComponentAimedAtASimulatedDeviceIsServedByIt(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateOutpost(ctx, "", storage.OutpostSpec{Name: "site-a"}, all, all); err != nil {
		t.Fatal(err)
	}
	panel, prof := lineProtocolProduct(t, ctx, gw)
	outpost := "site-a"
	for _, c := range []struct{ name, target string }{{"panel-1", "127.60.0.1:51325"}, {"aimed", "127.60.0.1:8080"}} {
		name := c.name
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name, ProductName: &panel}, all, all, all, all); err != nil {
			t.Fatal(err)
		}
		if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "tcp", Component: &name, Outpost: &outpost,
			Params: []byte(`{"target":"` + c.target + `"}`)}, all); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(api.NewHandler(gw, api.WithSimHub(&fakeHub{Registry: simhub.New()})))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	var view simulationBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/outposts/site-a/simulation", nil, http.StatusOK), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Devices) != 1 || view.Devices[0].Component != "panel-1" {
		t.Fatalf("site-a lists %+v, want panel-1 once and no row for the component aimed at it", view.Devices)
	}
	var sim componentSimBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/aimed/simulation", nil, http.StatusOK), &sim); err != nil {
		t.Fatal(err)
	}
	if !sim.Simulated || sim.ServedBy != "panel-1" || sim.Profile != prof.Name || sim.Address != "127.60.0.1" || sim.Activity == nil || sim.Activity.Component != "panel-1" {
		t.Fatalf("aimed simulation = %+v, want panel-1's device, served by it", sim)
	}

	// A caller who may read everything but update only the ALIAS must not
	// drive the owning device through it: the action scope is re-checked on
	// the owner the alias resolves to.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	tok, thash, tprefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	var pid string
	if err := conn.QueryRow(ctx, `insert into users (kind) values ('machine') returning id`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`insert into role (name, permissions) values ('alias-reader', '{"component:read","simulation:read"}'), ('alias-firer', '{"simulation:update"}')`, nil},
		{`insert into machine (user_id, name) values ($1, 'alias-only')`, []any{pid}},
		{`insert into credential (user_id, kind, secret_hash, prefix) values ($1, 'bearer', $2, $3)`, []any{pid, thash, tprefix}},
		{`insert into user_grant (user_id, role_id, scope_kind, scope_id) values ($1, (select id from role where name = 'alias-reader'), 'all', null)`, []any{pid}},
		{`insert into user_grant (user_id, role_id, scope_kind, scope_id) values ($1, (select id from role where name = 'alias-firer'), 'component', (select id::text from component where name = 'aimed'))`, []any{pid}},
	} {
		if _, err := conn.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
	}
	// A fresh server: the role index is read once per process.
	srv2 := httptest.NewServer(api.NewHandler(gw, api.WithSimHub(&fakeHub{Registry: simhub.New()})))
	defer srv2.Close()
	fireBody, _ := json.Marshal(map[string]any{"surface": "line", "command": "GET INPUT"})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv2.URL+"/api/v1/components/aimed/simulation:fire", bytes.NewReader(fireBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	rb, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	_ = rb
	// 409, 422 and 503 are answers from past the scope check (not simulated,
	// no such surface, no simulator): reaching one is the bypass. Today a
	// component-scoped simulation:update cannot be resolved against the
	// component table at all (the route is all-scope in practice), so the
	// request is refused before any of them; the owner re-check in resolve
	// keeps it so if that ever changes.
	switch resp.StatusCode {
	case http.StatusOK, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusServiceUnavailable:
		t.Fatalf("a caller scoped to the alias alone got past the scope check on the owner: status %d", resp.StatusCode)
	}
}
