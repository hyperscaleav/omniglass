package devseed_test

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// largeFleetFloor is what the large scenario promises: an enterprise-sized
// fleet, big enough that a list, a tree or a rollup that scales badly shows it.
const largeFleetFloor = 5000

// largeSeedBudget is the stated bound on seeding the large scenario on a fresh
// database, the number a throwaway stack's start-up pays.
const largeSeedBudget = 5 * time.Minute

func scenarioDoc(t *testing.T, name string) devseed.Doc {
	t.Helper()
	s, err := devseed.LookupScenario(name)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := s.Doc()
	if err != nil {
		t.Fatalf("scenario %s: %v", name, err)
	}
	return doc
}

// Every scenario's document passes the fixture's own shape check and keeps
// location names fleet-unique, since the seed finds an existing location by
// bare name and a repeated one would silently fold two places into one.
func TestScenarioDocsAreWellFormed(t *testing.T) {
	for _, s := range devseed.Scenarios() {
		t.Run(s.Name, func(t *testing.T) {
			if s.Empty {
				doc, err := s.Doc()
				if err != nil || !reflect.DeepEqual(doc, devseed.Doc{}) {
					t.Fatalf("empty scenario doc = %+v, %v; want nothing", doc, err)
				}
				return
			}
			doc := scenarioDoc(t, s.Name)
			checkDocShape(t, doc)
			seen := map[string]string{}
			for _, l := range doc.Locations {
				if prev, ok := seen[l.Name]; ok {
					t.Errorf("location name %q is used by keys %q and %q", l.Name, prev, l.Key)
				}
				seen[l.Name] = l.Key
			}
		})
	}
}

func TestScenarioDefaultIsTheExampleFleet(t *testing.T) {
	if got := devseed.Scenarios()[0].Name; got != "dev" {
		t.Fatalf("first scenario = %q, want dev (the default)", got)
	}
	base := fixturesDoc(t)
	if got := scenarioDoc(t, "dev"); !reflect.DeepEqual(got, base) {
		t.Fatal("dev scenario differs from the example fleet")
	}
}

func TestLookupScenarioRefusesAnUnknownNameWithTheKnownOnes(t *testing.T) {
	_, err := devseed.LookupScenario("enormous")
	if err == nil {
		t.Fatal("unknown scenario accepted")
	}
	for _, s := range devseed.Scenarios() {
		if !strings.Contains(err.Error(), s.Name) {
			t.Errorf("error %q does not list scenario %q", err, s.Name)
		}
	}
}

func TestLargeScenarioIsEnterpriseSized(t *testing.T) {
	if n := len(scenarioDoc(t, "large").Components); n < largeFleetFloor {
		t.Fatalf("large scenario has %d components, want at least %d", n, largeFleetFloor)
	}
}

// A copied room is the template room again: the same devices, the same
// systems, staffed the same way, and nothing staffed across rooms, which is
// what keeps a copy from reaching into a neighbour the way the divisible pair
// does.
func TestReplicateCopiesWholeRoomsOnly(t *testing.T) {
	base := fixturesDoc(t)
	p := devseed.ReplicaParams{Sites: 2, RoomCopies: 3}
	doc := devseed.Replicate(base, p)
	rooms := devseed.SelfContainedRooms(base)
	if len(rooms) == 0 {
		t.Fatal("the example fleet has no self-contained room to copy")
	}

	perRoom := map[string]int{}
	for _, c := range base.Components {
		perRoom[c.Location]++
	}
	want := 0
	for _, r := range rooms {
		want += perRoom[r]
	}
	if got := len(doc.Components) - len(base.Components); got != want*p.Sites*p.RoomCopies {
		t.Errorf("copied components = %d, want %d", got, want*p.Sites*p.RoomCopies)
	}

	compAt := map[string]string{}
	for _, c := range doc.Components {
		compAt[c.Key] = c.Location
	}
	sysAt := map[string]string{}
	for _, s := range doc.Systems {
		sysAt[s.Key] = s.Location
	}
	for _, ra := range doc.RoleAssignments[len(base.RoleAssignments):] {
		if sysAt[ra.System] != compAt[ra.Component] {
			t.Errorf("copied assignment %+v staffs a component from another room", ra)
		}
	}
	for _, m := range doc.Members[len(base.Members):] {
		if sysAt[m.System] != compAt[m.Component] {
			t.Errorf("copied membership %+v reaches another room", m)
		}
	}
	if !reflect.DeepEqual(doc, devseed.Replicate(base, p)) {
		t.Error("Replicate is not deterministic")
	}
	if !reflect.DeepEqual(devseed.Replicate(base, devseed.ReplicaParams{}), base) {
		t.Error("a zero-sized Replicate changed the fleet")
	}
}

// The degraded fleet raises only the severities and messages the example
// fleet's own alarms use, at most one per component, so it can never name a
// severity the boot seed does not ship or stack two conditions on one device.
func TestDegradeRaisesKnownConditionsOncePerComponent(t *testing.T) {
	base := fixturesDoc(t)
	doc := devseed.Degrade(base, devseed.DegradedEvery)
	known := map[string]bool{}
	for _, a := range base.Alarms {
		known[a.Severity+"|"+a.Message] = true
	}
	added := doc.Alarms[len(base.Alarms):]
	if len(added) < len(base.Components)/devseed.DegradedEvery/2 {
		t.Fatalf("degraded raised %d alarms over %d components, want roughly one in %d", len(added), len(base.Components), devseed.DegradedEvery)
	}
	per := map[string]int{}
	for _, a := range doc.Alarms {
		per[a.Component]++
	}
	for _, a := range added {
		if !known[a.Severity+"|"+a.Message] {
			t.Errorf("degraded alarm %+v is not one the example fleet uses", a)
		}
		if per[a.Component] != 1 {
			t.Errorf("component %q carries %d alarms", a.Component, per[a.Component])
		}
	}
}

func openSeededGateway(t *testing.T) (context.Context, *storage.PG, *pgx.Conn) {
	t.Helper()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("boot seed: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	return ctx, gw, conn
}

type fleetCounts struct{ locations, components, systems, assignments, alarms int }

func countFleet(t *testing.T, ctx context.Context, conn *pgx.Conn) fleetCounts {
	t.Helper()
	var c fleetCounts
	for q, dst := range map[string]*int{
		`select count(*) from location`:               &c.locations,
		`select count(*) from component`:              &c.components,
		`select count(*) from system`:                 &c.systems,
		`select count(*) from system_role_assignment`: &c.assignments,
		`select count(*) from alarm`:                  &c.alarms,
	} {
		if err := conn.QueryRow(ctx, q).Scan(dst); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	return c
}

// The replica path lands every copied row and a second run adds none, which is
// the property that matters for a generated fleet: its rooms repeat the
// template's platform-named devices hundreds of times, so a seed that cannot
// recognise its own rows would double the fleet on every start.
func TestRunScenarioReplicaIsIdempotent(t *testing.T) {
	ctx, gw, conn := openSeededGateway(t)
	s := devseed.ReplicaScenario(devseed.ReplicaParams{Sites: 1, RoomCopies: 2})
	doc, err := s.Doc()
	if err != nil {
		t.Fatal(err)
	}
	if err := devseed.RunScenario(ctx, gw, "", s, devseed.Options{SimFleet: true}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	first := countFleet(t, ctx, conn)
	if first.locations != len(doc.Locations) || first.systems != len(doc.Systems) {
		t.Errorf("seeded %d locations and %d systems, want %d and %d", first.locations, first.systems, len(doc.Locations), len(doc.Systems))
	}
	if err := devseed.RunScenario(ctx, gw, "", s, devseed.Options{SimFleet: true}); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if second := countFleet(t, ctx, conn); second != first {
		t.Errorf("second run changed the fleet: %+v, then %+v", first, second)
	}
	// A plain dev seed over the same database must not wire the copies after
	// the fact: the simulated fleet is the example fleet's devices, always.
	if err := devseed.Run(ctx, gw, ""); err != nil {
		t.Fatalf("dev run over the replica fleet: %v", err)
	}
	// The example fleet is simulated as it is for dev; no copied device is.
	var copied, all int
	if err := conn.QueryRow(ctx, `
		select count(*) filter (where l.name like '%-s001-%'), count(*)
		from endpoint e join component c on c.id = e.component
		join location l on l.id = c.location_id`).Scan(&copied, &all); err != nil {
		t.Fatal(err)
	}
	if copied != 0 || all == 0 {
		t.Errorf("simulated endpoints: %d on copied devices (want 0), %d in all (want some)", copied, all)
	}
}

func TestRunScenarioDegradedIsIdempotent(t *testing.T) {
	ctx, gw, conn := openSeededGateway(t)
	s, err := devseed.LookupScenario("degraded")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := devseed.RunScenario(ctx, gw, "", s, devseed.Options{}); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	doc, _ := s.Doc()
	// The breadth block's one raised-then-cleared pair rides beside the
	// fixture's alarms, as it does for dev (TestRunIdempotent).
	if got := countFleet(t, ctx, conn).alarms; got != len(doc.Alarms)+1 {
		t.Errorf("alarms = %d, want %d", got, len(doc.Alarms)+1)
	}
}

func TestRunScenarioEmptySeedsNothing(t *testing.T) {
	ctx, gw, conn := openSeededGateway(t)
	s, err := devseed.LookupScenario("empty")
	if err != nil {
		t.Fatal(err)
	}
	if err := devseed.RunScenario(ctx, gw, "", s, devseed.Options{SimFleet: true}); err != nil {
		t.Fatal(err)
	}
	if got := countFleet(t, ctx, conn); got != (fleetCounts{}) {
		t.Errorf("empty scenario seeded %+v", got)
	}
}

// The large scenario seeds within its stated budget on a fresh database.
func TestRunScenarioLargeWithinBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("seeds the full large fleet")
	}
	ctx, gw, conn := openSeededGateway(t)
	s, err := devseed.LookupScenario("large")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := devseed.RunScenario(ctx, gw, "", s, devseed.Options{}); err != nil {
		t.Fatal(err)
	}
	took := time.Since(start)
	t.Logf("large scenario seeded in %s", took.Round(time.Second))
	if took > largeSeedBudget {
		t.Errorf("large scenario took %s, budget %s", took, largeSeedBudget)
	}
	if got := countFleet(t, ctx, conn).components; got < largeFleetFloor {
		t.Errorf("large scenario landed %d components, want at least %d", got, largeFleetFloor)
	}
}
