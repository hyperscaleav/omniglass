package api_test

import (
	"context"
	"encoding/json"
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
	"github.com/hyperscaleav/omniglass/internal/seed"
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
		node string
		op   collection.SimOperation
	}
	fireErr error
}

func (h *fakeHub) SimStatus(node string) (simhub.Status, bool) { return h.Status(node) }

func (h *fakeHub) FireSimOperation(_ context.Context, node string, op collection.SimOperation) (collection.SimOperationResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fired = append(h.fired, struct {
		node string
		op   collection.SimOperation
	}{node, op})
	if h.fireErr != nil {
		return collection.SimOperationResult{}, h.fireErr
	}
	return collection.SimOperationResult{Body: "INPUT hdmi2", TookMs: 1}, nil
}

type fleetBody struct {
	Connected bool `json:"connected"`
	Nodes     []struct {
		Node      string `json:"node"`
		Connected bool   `json:"connected"`
		Devices   []struct {
			Component string `json:"component"`
			Address   string `json:"address"`
		} `json:"devices"`
	} `json:"nodes"`
}

type componentSimBody struct {
	Simulated bool   `json:"simulated"`
	Node      string `json:"node"`
	Product   string `json:"product"`
	Profile   string `json:"profile"`
	Address   string `json:"address"`
	Surface   []struct {
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
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	for _, n := range []string{"site-a", "site-b"} {
		if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: n}, all, all); err != nil {
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
	comp, node := "panel-1", "site-a"
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "tcp", Component: &comp, Node: &node,
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
	if fleet.Connected || len(fleet.Nodes) != 0 {
		t.Fatalf("before any simulator: %+v", fleet)
	}

	hub.Pulled("site-a", collection.SimCapabilities{PortOffset: 10000}, []string{panelID})
	get(viewer, "/simulation", &fleet)
	if !fleet.Connected || len(fleet.Nodes) != 1 || fleet.Nodes[0].Node != "site-a" || !fleet.Nodes[0].Connected ||
		len(fleet.Nodes[0].Devices) != 1 || fleet.Nodes[0].Devices[0].Component != "panel-1" {
		t.Fatalf("fleet = %+v, want site-a's simulator serving panel-1", fleet)
	}
	c.do("", http.MethodGet, "/simulation", nil, http.StatusUnauthorized)

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
	if !sim.Simulated || sim.Node != "site-a" || sim.Product != panel || sim.Profile != prof.Name || sim.Address != "127.60.0.1" {
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
	if fired.Body != "INPUT hdmi2" || len(hub.fired) != 1 || hub.fired[0].node != "site-a" ||
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

	// Add a simulated device from a profile: a component of the product,
	// at a free address in the simulated block, with the endpoint the
	// planner picks (the NVP attach, no secret needed), on the node.
	var added struct {
		Component string `json:"component"`
		Address   string `json:"address"`
		Endpoints int    `json:"endpoints"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/nodes/site-a/simulation/devices",
		map[string]any{"name": "panel-9", "product": panel}, http.StatusCreated), &added); err != nil {
		t.Fatal(err)
	}
	if added.Component != "panel-9" || added.Address != "127.60.0.2" || added.Endpoints != 1 {
		t.Fatalf("added = %+v, want panel-9 at the free 127.60.0.2 with one endpoint", added)
	}
	var driver, onNode string
	if err := conn.QueryRow(ctx, `select coalesce(d.name, ''), n.name from endpoint e join node n on n.principal_id = e.node_name
		left join driver d on d.id = e.driver_id where e.component = (select id from component where name = 'panel-9')`).Scan(&driver, &onNode); err != nil {
		t.Fatal(err)
	}
	if driver != "newtron-nvp" || onNode != "site-a" {
		t.Fatalf("panel-9's endpoint is %q on %q, want a newtron-nvp attach on site-a", driver, onNode)
	}
	var withNew simulationBody
	get(ownerTok, "/nodes/site-a/simulation", &withNew)
	if len(withNew.Devices) != 2 {
		t.Fatalf("site-a lists %d devices after the add, want 2", len(withNew.Devices))
	}
	c.do(viewer, http.MethodPost, "/nodes/site-a/simulation/devices", map[string]any{"name": "panel-10", "product": panel}, http.StatusForbidden)
	out := c.do(ownerTok, http.MethodPost, "/nodes/site-a/simulation/devices", map[string]any{"name": "panel-10", "product": "generic-device"}, http.StatusUnprocessableEntity)
	if !strings.Contains(string(out), "simulat") {
		t.Fatalf("an unsimulated product was refused with %s, want it to say why", out)
	}
	c.do(ownerTok, http.MethodPost, "/nodes/site-b/simulation/devices", map[string]any{"name": "panel-11", "product": panel}, http.StatusConflict)

	// A component name repeats across places: the add wires the component it
	// created, never a namesake elsewhere.
	for _, l := range []string{"loc-a", "loc-b"} {
		if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: l, LocationType: "campus"}, all); err != nil {
			t.Fatal(err)
		}
	}
	locA := "loc-a"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "dup", ProductName: &panel, LocationName: &locA}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	c.do(ownerTok, http.MethodPost, "/nodes/site-a/simulation/devices", map[string]any{"name": "dup", "product": panel, "location": "loc-b"}, http.StatusCreated)
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

// lineProtocolProduct is a seeded product on the newtron-nvp driver that a
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
		if p.DriverName == nil || *p.DriverName != "newtron-nvp" {
			continue
		}
		if d, ok := store.Devices[p.Name]; ok {
			return p.Name, d
		}
	}
	t.Fatal("no seeded newtron-nvp product is a simulator profile's own product")
	return "", nil
}
