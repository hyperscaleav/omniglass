package storage_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// healthSeries reads one owner's recorded health values oldest-first, the same
// order and tie-break the recompute reads them back in.
func (f *healthFixture) healthSeries(t *testing.T, ctx context.Context, ownerKind, ownerID string) []string {
	t.Helper()
	col := map[string]string{"component": "component_id", "system": "system_id", "location": "location_id"}[ownerKind]
	if col == "" {
		t.Fatalf("unknown owner kind %q", ownerKind)
	}
	rows, err := f.conn.Query(ctx, `select value from state_datapoint
		where `+col+` = (select id from `+ownerKind+` where name = $1)
		  and key = 'health' order by ts asc, id asc`, ownerID)
	if err != nil {
		t.Fatalf("read health series %s/%s: %v", ownerKind, ownerID, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan health series %s/%s: %v", ownerKind, ownerID, err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate health series %s/%s: %v", ownerKind, ownerID, err)
	}
	return out
}

// assertTransitionOnly is the invariant of the whole recording design, checked
// over EVERY owner in the database rather than the one a test happens to be
// about: two consecutive rows with the same value are impossible, because a row
// is a change and a change that changes nothing is not one. It runs on the raw
// rows, so it holds no matter which trigger wrote them or how many recomputes
// one transaction ran.
func (f *healthFixture) assertTransitionOnly(t *testing.T, ctx context.Context) {
	t.Helper()
	type row struct{ owner, kind, value string }
	// The three estate arcs store ids and the node arc still stores a name, so the
	// owner resolves back to a NAME here: the invariant groups by owner, and a
	// failure message naming the entity is worth more than one printing a uuid.
	rows, err := f.conn.Query(ctx, `
		select coalesce(c.name, s.name, l.name, n.name) as owner, sd.owner_kind, sd.value
		from state_datapoint sd
		left join component c on c.id = sd.component_id
		left join system    s on s.id = sd.system_id
		left join location  l on l.id = sd.location_id
		left join node      n on n.principal_id = sd.node_id
		where sd.key = 'health'
		order by owner, sd.id asc`)
	if err != nil {
		t.Fatalf("read all health rows: %v", err)
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.owner, &r.kind, &r.value); err != nil {
			rows.Close()
			t.Fatalf("scan health row: %v", err)
		}
		all = append(all, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate health rows: %v", err)
	}

	for i := 1; i < len(all); i++ {
		if all[i] == all[i-1] {
			t.Errorf("%s/%s recorded %q twice in a row: health is transition-only, "+
				"a second identical value is a sample, not an edge (series %v)",
				all[i].kind, all[i].owner, all[i].value, f.healthSeries(t, ctx, all[i].kind, all[i].owner))
		}
	}
}

// TestHealthRecordsOneRowPerChange is the live-database regression, replayed
// through the Gateway exactly as it happened: a system conforming to the seeded
// meeting-room standard (room-mic wants microphone and speaker at quorum 2,
// main-display wants a flat panel at quorum 1), three components, then the
// assignments one at a time. Only two verdicts ever occur (degraded from the
// moment it exists, healthy once both roles are staffed), so the record must hold
// exactly two rows. The live run held three, the middle one a duplicate degraded.
func TestHealthRecordsOneRowPerChange(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	std := "meeting-room"
	if _, err := f.gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "hq-boardroom", StandardID: &std,
	}, f.all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	if got := f.healthSeries(t, ctx, "system", "hq-boardroom"); !sameSeq(got, []string{"degraded"}) {
		t.Fatalf("after create = %v, want the one opening degraded (both roles unstaffed)", got)
	}

	bar, panel := "cisco-room-bar", "samsung-qm55"
	for _, c := range []struct{ name, product string }{
		{"bar-a", bar}, {"bar-b", bar}, {"panel-a", panel},
	} {
		product := c.product
		if _, err := f.gw.CreateComponent(ctx, "", storage.ComponentSpec{
			Name: c.name, ProductName: &product,
		}, f.all); err != nil {
			t.Fatalf("create component %s: %v", c.name, err)
		}
	}

	// The verdict after each assignment, from the roles themselves: one bar leaves
	// room-mic below its quorum of 2, two bars satisfy it but main-display is still
	// empty, and only the panel makes the system whole.
	steps := []struct{ role, component, want string }{
		{"room-mic", "bar-a", "degraded"},
		{"room-mic", "bar-b", "degraded"},
		{"main-display", "panel-a", "healthy"},
	}
	for _, s := range steps {
		if err := f.gw.AssignRole(ctx, "", "hq-boardroom", s.role, s.component, f.all); err != nil {
			t.Fatalf("assign %s to %s: %v", s.component, s.role, err)
		}
		if _, v := f.recorded(t, ctx, "system", "hq-boardroom"); v != s.want {
			t.Errorf("after assigning %s to %s the record says %q, want %q", s.component, s.role, v, s.want)
		}
	}

	if got := f.healthSeries(t, ctx, "system", "hq-boardroom"); !sameSeq(got, []string{"degraded", "healthy"}) {
		t.Errorf("series = %v, want [degraded healthy]: the staffing steps that changed no verdict must record nothing",
			got)
	}
	f.assertTransitionOnly(t, ctx)
}

// TestHealthRecordsEveryRealChange is the mirror of the duplicate: a verdict that
// really moves must always land a row. The two failure modes share a mechanism, a
// recorded value that does not match what the roles say, so they are asserted
// against each other here: after every write the recorded verdict and the one the
// report computes from the live roles must be the same string.
func TestHealthRecordsEveryRealChange(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	std := "meeting-room"
	if _, err := f.gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "hq-boardroom", StandardID: &std,
	}, f.all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	bar, panel := "cisco-room-bar", "samsung-qm55"
	for _, c := range []struct{ name, product string }{
		{"bar-a", bar}, {"bar-b", bar}, {"panel-a", panel},
	} {
		product := c.product
		if _, err := f.gw.CreateComponent(ctx, "", storage.ComponentSpec{
			Name: c.name, ProductName: &product,
		}, f.all); err != nil {
			t.Fatalf("create component %s: %v", c.name, err)
		}
	}
	for _, s := range []struct{ role, component string }{
		{"room-mic", "bar-a"}, {"room-mic", "bar-b"}, {"main-display", "panel-a"},
	} {
		if err := f.gw.AssignRole(ctx, "", "hq-boardroom", s.role, s.component, f.all); err != nil {
			t.Fatalf("assign %s to %s: %v", s.component, s.role, err)
		}
	}
	f.mustAgreeWithRecord(t, ctx, "hq-boardroom", "healthy")

	// An alarm that takes microphone from one bar drops room-mic to one satisfying
	// component, below its quorum: a real change, so a real row.
	alarm, err := f.gw.RaiseAlarm(ctx, "", "bar-a", storage.AlarmSpec{
		Severity: "warning", Message: "mic array not responding", Capabilities: []string{"microphone"},
	})
	if err != nil {
		t.Fatalf("raise alarm: %v", err)
	}
	f.mustAgreeWithRecord(t, ctx, "hq-boardroom", "degraded")

	// Clearing it puts the system back, and the whole series is still edges only.
	if err := f.gw.ClearAlarm(ctx, "", "bar-a", alarm.ID); err != nil {
		t.Fatalf("clear alarm: %v", err)
	}
	f.mustAgreeWithRecord(t, ctx, "hq-boardroom", "healthy")

	if got := f.healthSeries(t, ctx, "system", "hq-boardroom"); !sameSeq(got,
		[]string{"degraded", "healthy", "degraded", "healthy"}) {
		t.Errorf("series = %v, want [degraded healthy degraded healthy]", got)
	}
	f.assertTransitionOnly(t, ctx)
}

// concurrentRooms is how many rooms the race proofs drive at once. Two writes
// only collide while they overlap in time, and pgxpool opens its connections
// lazily, so a single pair launched together mostly takes turns while the second
// goroutine is still dialing. A handful of rooms firing together fills the pool
// and makes the overlap the point of the test rather than a coin toss.
const concurrentRooms = 8

// staffPair builds one room of the estate the race proofs need: a system
// conforming to a shared standard whose single role sits at quorum 2, staffed by
// two room bars, so the system reads healthy and EITHER bar failing drops the
// role below quorum. Two writes that move the same system are what the record has
// to survive.
func (f *healthFixture) staffPair(t *testing.T, ctx context.Context, standard, system string, components ...string) {
	t.Helper()
	room := "hq-r1"
	if _, err := f.gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: system, StandardID: &standard, LocationName: &room,
	}, f.all); err != nil {
		t.Fatalf("create system %s: %v", system, err)
	}
	bar := "cisco-room-bar"
	for _, c := range components {
		product := bar
		if _, err := f.gw.CreateComponent(ctx, "", storage.ComponentSpec{
			Name: c, ProductName: &product,
		}, f.all); err != nil {
			t.Fatalf("create component %s: %v", c, err)
		}
		if err := f.gw.AssignRole(ctx, "", system, "pair", c, f.all); err != nil {
			t.Fatalf("assign %s: %v", c, err)
		}
	}
	if _, v := f.recorded(t, ctx, "system", system); v != "healthy" {
		t.Fatalf("staffed system %s = %q, want healthy", system, v)
	}
}

// pairStandard declares the shared quorum-2 standard the race rooms conform to.
func (f *healthFixture) pairStandard(t *testing.T, ctx context.Context, id string) string {
	t.Helper()
	if err := f.gw.UpsertStandard(ctx, storage.Standard{ID: id, DisplayName: "Pair"}); err != nil {
		t.Fatalf("create standard: %v", err)
	}
	if _, err := f.gw.SetSystemRole(ctx, "", "standard", id, storage.SystemRoleSpec{
		Name: "pair", DisplayName: "Pair", Quorum: 2,
		Capabilities: []string{"microphone", "speaker"}, Impact: "degraded",
	}); err != nil {
		t.Fatalf("declare role: %v", err)
	}
	return id
}

// runTogether releases every one of these writes at the same instant and fails on
// the first error. The barrier is the whole point: the defects below are only
// reachable while two transactions are open at once.
func runTogether(t *testing.T, writes ...func() error) {
	t.Helper()
	start := make(chan struct{})
	errs := make([]error, len(writes))
	var wg sync.WaitGroup
	for i, w := range writes {
		wg.Add(1)
		go func(i int, w func() error) {
			defer wg.Done()
			<-start
			errs[i] = w()
		}(i, w)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent write %d: %v", i, err)
		}
	}
}

// TestHealthConcurrentWritesRecordOneEdge is the live duplicate's mechanism. Two
// writes that each move the same system to the same verdict overlap in time, so
// each reads the system's last recorded value before the other has committed,
// each sees the OLD one, and each concludes it is recording a change. One
// transition, two rows, which is exactly the pair of consecutive degraded rows
// the live database holds.
//
// Nothing about the sequence is exotic: two alarms on two components in one room
// is a normal minute in an estate, and a console that saves a panel fires its
// writes together. The locations above the rooms make the same point harder,
// since every one of these writes rolls up through the same three of them.
func TestHealthConcurrentWritesRecordOneEdge(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()
	std := f.pairStandard(t, ctx, "pair-standard")

	rooms := make([]string, 0, concurrentRooms)
	writes := make([]func() error, 0, 2*concurrentRooms)
	for i := 0; i < concurrentRooms; i++ {
		system := fmt.Sprintf("pair-sys-%d", i)
		a, b := system+"-a", system+"-b"
		f.staffPair(t, ctx, std, system, a, b)
		rooms = append(rooms, system)
		// Both alarms take microphone, which the role requires, so each on its own
		// already drops the role below its quorum of 2: one transition per room,
		// whichever lands first, and nothing left for the second to record.
		for _, c := range []string{a, b} {
			component := c
			writes = append(writes, func() error {
				_, err := f.gw.RaiseAlarm(ctx, "", component, storage.AlarmSpec{
					Severity: "warning", Message: "mic array not responding",
					Capabilities: []string{"microphone"},
				})
				return err
			})
		}
	}

	before := map[string]int{}
	for _, s := range rooms {
		before[s], _ = f.recorded(t, ctx, "system", s)
	}
	runTogether(t, writes...)

	for _, s := range rooms {
		n, v := f.recorded(t, ctx, "system", s)
		if v != "degraded" {
			t.Errorf("%s = %q, want degraded", s, v)
		}
		if n-before[s] != 1 {
			t.Errorf("%s: two overlapping writes recorded %d rows for one transition, want exactly 1 (series %v)",
				s, n-before[s], f.healthSeries(t, ctx, "system", s))
		}
	}
	f.assertTransitionOnly(t, ctx)
}

// TestHealthConcurrentOppositeWritesLeaveNoStaleRecord is the second live
// symptom, and the proof it is the same defect. When one write moves the system
// and another moves it back at the same moment, the loser's verdict was computed
// from a snapshot taken before the winner committed, so the record can settle on
// a value the roles no longer support. Once it does, the next write that really
// changes the verdict finds the record already holding it and writes nothing: a
// real transition, silently missing, exactly as observed.
func TestHealthConcurrentOppositeWritesLeaveNoStaleRecord(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()
	std := f.pairStandard(t, ctx, "race-standard")

	rooms := make([]string, 0, concurrentRooms)
	writes := make([]func() error, 0, 2*concurrentRooms)
	for i := 0; i < concurrentRooms; i++ {
		system := fmt.Sprintf("race-sys-%d", i)
		a, b := system+"-a", system+"-b"
		f.staffPair(t, ctx, std, system, a, b)
		rooms = append(rooms, system)

		// A standing alarm on one bar puts the room degraded. Clearing it is the write
		// that says healthy, and an alarm on the other bar is the write that says
		// degraded: run together, they disagree about what the estate is.
		standing, err := f.gw.RaiseAlarm(ctx, "", a, storage.AlarmSpec{
			Severity: "warning", Message: "mic array not responding", Capabilities: []string{"microphone"},
		})
		if err != nil {
			t.Fatalf("raise standing alarm: %v", err)
		}
		if _, v := f.recorded(t, ctx, "system", system); v != "degraded" {
			t.Fatalf("%s = %q, want degraded", system, v)
		}
		alarmed, healed, id := b, a, standing.ID
		writes = append(writes,
			func() error { return f.gw.ClearAlarm(ctx, "", healed, id) },
			func() error {
				_, err := f.gw.RaiseAlarm(ctx, "", alarmed, storage.AlarmSpec{
					Severity: "warning", Message: "speaker dead", Capabilities: []string{"speaker"},
				})
				return err
			})
	}
	runTogether(t, writes...)

	// Whatever order they landed in, every room now has exactly one alarmed bar, so
	// its role is below quorum and the system is degraded. The record has to say so:
	// a record that has drifted from the roles swallows the NEXT real transition.
	for _, s := range rooms {
		f.mustAgreeWithRecord(t, ctx, s, "degraded")
	}
	f.assertTransitionOnly(t, ctx)
}

// TestHealthInvariantAcrossEveryTrigger is the standing guard rather than a
// proof about one defect. It drives every write in the trigger set, in an order
// nobody would plan, and asserts the one thing that must be true of the record
// afterwards no matter which triggers ran, how many entities a single write
// touched, or how many recomputes happened inside one transaction: no owner ever
// holds the same value in two consecutive rows.
//
// The trigger set is the part of this slice that grows. A future trigger that
// recomputes a fifth kind of thing, or an existing one taught to recompute two,
// belongs in the list below and inherits the assertion for free.
func TestHealthInvariantAcrossEveryTrigger(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	std := f.pairStandard(t, ctx, "sweep-standard")
	f.staffPair(t, ctx, std, "sweep-sys", "sweep-a", "sweep-b")
	bar, panel := "cisco-room-bar", "samsung-qm55"
	empty, room2 := "", "hq-r2"
	f.mustLocation(t, ctx, "hq-r2", "room", ptrStr("hq-b1"))

	var alarm *storage.Alarm
	for _, step := range []struct {
		what string
		do   func() error
	}{
		{"raise an alarm on a capability the role needs", func() error {
			a, err := f.gw.RaiseAlarm(ctx, "", "sweep-a", storage.AlarmSpec{
				Severity: "warning", Message: "mic dead", Capabilities: []string{"microphone"}})
			alarm = a
			return err
		}},
		{"raise a second alarm that decides nothing", func() error {
			_, err := f.gw.RaiseAlarm(ctx, "", "sweep-a", storage.AlarmSpec{
				Severity: "info", Message: "fan noise", Capabilities: []string{"speaker"}})
			return err
		}},
		{"clear the deciding alarm", func() error { return f.gw.ClearAlarm(ctx, "", "sweep-a", alarm.ID) }},
		{"suppress a required capability", func() error {
			return f.gw.SetComponentCapability(ctx, "", "sweep-b", "microphone", false)
		}},
		{"restore it", func() error { return f.gw.ClearComponentCapability(ctx, "", "sweep-b", "microphone") }},
		{"unstaff the role", func() error { return f.gw.UnassignRole(ctx, "", "sweep-sys", "pair", "sweep-b", f.all) }},
		{"staff it again", func() error { return f.gw.AssignRole(ctx, "", "sweep-sys", "pair", "sweep-b", f.all) }},
		{"raise the quorum on the standard, moving every conforming system", func() error {
			_, err := f.gw.SetSystemRole(ctx, "", "standard", std, storage.SystemRoleSpec{
				Name: "pair", DisplayName: "Pair", Quorum: 3,
				Capabilities: []string{"microphone", "speaker"}, Impact: "outage"})
			return err
		}},
		{"lower it back", func() error {
			_, err := f.gw.SetSystemRole(ctx, "", "standard", std, storage.SystemRoleSpec{
				Name: "pair", DisplayName: "Pair", Quorum: 2,
				Capabilities: []string{"microphone", "speaker"}, Impact: "degraded"})
			return err
		}},
		{"declare a second role on the system itself", func() error {
			_, err := f.gw.SetSystemRole(ctx, "", "system", "sweep-sys", storage.SystemRoleSpec{
				Name: "screen", DisplayName: "Screen", Quorum: 1,
				Capabilities: []string{"flat-panel-display"}, Impact: "outage"})
			return err
		}},
		{"withdraw it", func() error { return f.gw.DeleteSystemRole(ctx, "", "system", "sweep-sys", "screen") }},
		{"swap a staffing component's product out from under it", func() error {
			_, err := f.gw.UpdateComponent(ctx, "", "sweep-a", storage.ComponentPatch{ProductName: &panel}, f.all, f.all)
			return err
		}},
		{"swap it back", func() error {
			_, err := f.gw.UpdateComponent(ctx, "", "sweep-a", storage.ComponentPatch{ProductName: &bar}, f.all, f.all)
			return err
		}},
		{"relocate the system, which recomputes both ends", func() error {
			_, err := f.gw.UpdateSystem(ctx, "", "sweep-sys", storage.SystemPatch{LocationName: &room2}, f.all, f.all)
			return err
		}},
		{"convert it to a one-off, dropping the inherited role", func() error {
			_, err := f.gw.UpdateSystem(ctx, "", "sweep-sys", storage.SystemPatch{StandardID: &empty}, f.all, f.all)
			return err
		}},
		{"classify it again", func() error {
			_, err := f.gw.UpdateSystem(ctx, "", "sweep-sys", storage.SystemPatch{StandardID: &std}, f.all, f.all)
			return err
		}},
		{"create a second conforming system in the same room", func() error {
			_, err := f.gw.CreateSystem(ctx, "", storage.SystemSpec{
				Name: "sweep-sys-2", StandardID: &std, LocationName: &room2}, f.all)
			return err
		}},
	} {
		if err := step.do(); err != nil {
			t.Fatalf("%s: %v", step.what, err)
		}
		f.assertTransitionOnly(t, ctx)
	}

	// The record still tracks the roles at the end of all that: the never-cleared
	// second alarm still holds speaker away from one bar, so the role is one short
	// of its quorum, and the system created at the end has nobody in it at all.
	f.mustAgreeWithRecord(t, ctx, "sweep-sys", "degraded")
	f.mustAgreeWithRecord(t, ctx, "sweep-sys-2", "degraded")
}

// A delete is a health event for everything upstream of the deleted row, and the
// record has to show it. Removing the system that was dragging a location down
// improves that location, which is an edge exactly as real as the failure that
// caused it: an operator reading the location's history has to see the recovery,
// not a verdict frozen at the moment its worst system was deleted.
func TestHealthRecordsDeleteRipple(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	// hq-r1 already holds hq-huddle from the fixture. Add a second system and break
	// it, so the room is degraded because of THIS system and nothing else.
	std := f.pairStandard(t, ctx, "ripple-standard")
	f.staffPair(t, ctx, std, "ripple-sys", "ripple-a", "ripple-b")
	if _, err := f.gw.RaiseAlarm(ctx, "", "ripple-a", storage.AlarmSpec{
		Severity: "warning", Message: "mic dead", Capabilities: []string{"microphone"}}); err != nil {
		t.Fatalf("raise: %v", err)
	}
	if _, v := f.recorded(t, ctx, "location", "hq-r1"); v != "degraded" {
		t.Fatalf("room with a broken system = %q, want degraded", v)
	}

	// Deleting the broken system is the room's recovery.
	if err := f.gw.DeleteSystem(ctx, "", "ripple-sys", f.all, f.all); err != nil {
		t.Fatalf("delete system: %v", err)
	}
	if _, v := f.recorded(t, ctx, "location", "hq-r1"); v != "healthy" {
		t.Errorf("room after its broken system was deleted = %q, want healthy: "+
			"the delete improved the location and that edge went unrecorded (series %v)",
			v, f.healthSeries(t, ctx, "location", "hq-r1"))
	}
	f.assertTransitionOnly(t, ctx)
}

// A product is a contract, so editing one reaches every component built to it.
// Withdrawing a capability the product promised can drop a role below quorum in
// systems nobody touched, and that is a real transition in each of them.
func TestHealthRecordsProductCapabilityRipple(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	// The test owns its product: the seeded catalog is official and an official row
	// refuses edits, which is the whole mechanism under test here.
	if _, err := f.gw.CreateProduct(ctx, "", storage.Product{
		Name: "ripple-bar", DisplayName: "Ripple Bar", VendorID: ptrStr("cisco"),
		Capabilities: []string{"microphone", "speaker"},
	}); err != nil {
		t.Fatalf("create product: %v", err)
	}
	std := f.pairStandard(t, ctx, "product-standard")
	room := "hq-r1"
	if _, err := f.gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "product-sys", StandardID: &std, LocationName: &room}, f.all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	product := "ripple-bar"
	for _, c := range []string{"product-a", "product-b"} {
		if _, err := f.gw.CreateComponent(ctx, "", storage.ComponentSpec{
			Name: c, ProductName: &product}, f.all); err != nil {
			t.Fatalf("create component %s: %v", c, err)
		}
		if err := f.gw.AssignRole(ctx, "", "product-sys", "pair", c, f.all); err != nil {
			t.Fatalf("assign %s: %v", c, err)
		}
	}
	if _, v := f.recorded(t, ctx, "system", "product-sys"); v != "healthy" {
		t.Fatalf("staffed system = %q, want healthy", v)
	}

	// Take microphone away from the product both staffing components are built to,
	// and neither can fill a role that requires it.
	speakerOnly := []string{"speaker"}
	if _, err := f.gw.UpdateProduct(ctx, "", "ripple-bar", storage.ProductPatch{
		Capabilities: &speakerOnly}); err != nil {
		t.Fatalf("withdraw capability: %v", err)
	}
	if _, v := f.recorded(t, ctx, "system", "product-sys"); v != "degraded" {
		t.Errorf("system after its product lost a required capability = %q, want degraded: "+
			"the product edit broke the role and that edge went unrecorded (series %v)",
			v, f.healthSeries(t, ctx, "system", "product-sys"))
	}
	f.assertTransitionOnly(t, ctx)
}

// mustAgreeWithRecord holds the record and the report to the same answer. A read
// that computes degraded over a record that still says healthy means a real
// transition went unwritten, which is the same defect as writing one twice: the
// recorded value stopped tracking the verdict.
func (f *healthFixture) mustAgreeWithRecord(t *testing.T, ctx context.Context, systemName, want string) {
	t.Helper()
	rep, err := f.gw.SystemHealth(ctx, systemName, time.Time{}, f.all)
	if err != nil {
		t.Fatalf("system health %s: %v", systemName, err)
	}
	if rep.Verdict != want {
		t.Fatalf("reported verdict = %q, want %q (roles %+v)", rep.Verdict, want, rep.Roles)
	}
	_, recorded := f.recorded(t, ctx, "system", systemName)
	if recorded != rep.Verdict {
		t.Errorf("recorded verdict %q disagrees with the reported %q: a real transition went unrecorded (series %v)",
			recorded, rep.Verdict, strings.Join(f.healthSeries(t, ctx, "system", systemName), " -> "))
	}
}
