package storage_test

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// Driver tasks re-derive when a driver's spec changes (#823): an attached
// endpoint runs the spec the driver row carries now, not the spec as of
// attach. Skipped under -short.

type taskRow struct {
	ID        string
	Mode      string
	Spec      string
	CreatedAt time.Time
}

func endpointTasks(t *testing.T, ctx context.Context, conn *pgx.Conn, endpointID string) []taskRow {
	t.Helper()
	rows, err := conn.Query(ctx, `select id, mode, spec::text, created_at from task where endpoint_id = $1 order by id`, endpointID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []taskRow
	for rows.Next() {
		var r taskRow
		if err := rows.Scan(&r.ID, &r.Mode, &r.Spec, &r.CreatedAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func rederiveFixture(t *testing.T) (context.Context, *storage.PG, *pgx.Conn, *storage.Endpoint) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-r"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-r"}, all, all); err != nil {
		t.Fatal(err)
	}
	comp, drv, node := "panel-r", "newtron-nvp", "edge-r"
	ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &drv, Component: &comp, Node: &node,
		Inputs: map[string]string{"host": "10.20.4.60"}, SecretRead: all, CanAdmin: true,
	}, all)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(ctx) })
	return ctx, gw, conn, ep
}

// A re-seed with nothing changed touches no task and no endpoint: the derived
// ids are content hashes, so the same spec re-derives the same rows.
func TestReseedWithNoChangeTouchesNothing(t *testing.T) {
	ctx, gw, conn, ep := rederiveFixture(t)
	before := endpointTasks(t, ctx, conn, ep.ID)
	var epUpdated time.Time
	if err := conn.QueryRow(ctx, `select updated_at from endpoint where id = $1`, ep.ID).Scan(&epUpdated); err != nil {
		t.Fatal(err)
	}
	// Review of #823: an unchanged re-seed issues no write at all for the
	// driver or its tasks; the rows' xmin (the writing transaction) stays.
	xmins := func() string {
		var drv, tasks string
		if err := conn.QueryRow(ctx, `select xmin::text from driver where name = 'newtron-nvp'`).Scan(&drv); err != nil {
			t.Fatal(err)
		}
		if err := conn.QueryRow(ctx, `select coalesce(string_agg(xmin::text, ',' order by id), '') from task where endpoint_id = $1`, ep.ID).Scan(&tasks); err != nil {
			t.Fatal(err)
		}
		return drv + "|" + tasks
	}
	xminBefore := xmins()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	if got := xmins(); got != xminBefore {
		t.Fatalf("an unchanged re-seed wrote the driver or its tasks (xmin %s -> %s)", xminBefore, got)
	}
	after := endpointTasks(t, ctx, conn, ep.ID)
	if len(after) != len(before) {
		t.Fatalf("a no-change re-seed moved the tasks from %d to %d", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("a no-change re-seed touched task %s: %+v -> %+v", before[i].ID, before[i], after[i])
		}
	}
	var epAfter time.Time
	if err := conn.QueryRow(ctx, `select updated_at from endpoint where id = $1`, ep.ID).Scan(&epAfter); err != nil {
		t.Fatal(err)
	}
	if !epAfter.Equal(epUpdated) {
		t.Fatal("a no-change re-seed bumped the endpoint (its config generation)")
	}
}

// A reply and error shape added to an official spec reaches an endpoint that
// was attached before it: the seed re-derives, the superseded tasks retire.
func TestSeededSpecChangeReachesAttachedEndpoints(t *testing.T) {
	ctx, gw, conn, ep := rederiveFixture(t)
	// Roll the official spec back to one without reply shapes, as an older
	// binary's seed carried it.
	d, err := gw.GetDriver(ctx, "newtron-nvp")
	if err != nil {
		t.Fatal(err)
	}
	old := strings.NewReplacer(`,"reply":{"error":{"prefix":"-ERR"},"prefix":"INPUT "}`, "").Replace(compactJSON(t, d.Spec))
	if old == compactJSON(t, d.Spec) {
		t.Fatalf("the seeded spec carries no reply shape to roll back: %s", d.Spec)
	}
	if err := gw.UpsertDriver(ctx, storage.Driver{Name: d.Name, Label: d.Label, Version: d.Version, Official: true, Spec: []byte(old)}); err != nil {
		t.Fatal(err)
	}
	for _, task := range endpointTasks(t, ctx, conn, ep.ID) {
		if strings.Contains(task.Spec, `"reply"`) {
			t.Fatalf("after the rollback a task still carries a reply shape: %s", task.Spec)
		}
	}
	// The current binary's seed carries the shapes; it must reach the endpoint.
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	wl, err := gw.NodeWorklist(ctx, "edge-r")
	if err != nil {
		t.Fatal(err)
	}
	var polls, listens int
	for _, task := range wl.Tasks {
		switch task.Mode {
		case "poll":
			if strings.Contains(string(task.Spec), `"driver"`) {
				polls++
				if !strings.Contains(string(task.Spec), `"error":{"prefix":"-ERR"}`) && !strings.Contains(compactJSON(t, task.Spec), `"error":{"prefix":"-ERR"}`) {
					t.Fatalf("the attached endpoint's poll did not gain the error shape: %s", task.Spec)
				}
			}
		case "listen":
			listens++
		}
	}
	// One standing listen task per listener the shipped spec declares, so
	// the count follows the spec rather than a number written here.
	var shipped struct {
		Listeners []json.RawMessage `json:"listeners"`
	}
	if err := json.Unmarshal(d.Spec, &shipped); err != nil {
		t.Fatal(err)
	}
	if polls != 1 || listens != len(shipped.Listeners) {
		t.Fatalf("worklist holds %d driver polls and %d listens, want 1 and %d (the superseded retired)", polls, listens, len(shipped.Listeners))
	}
}

// A custom driver's spec update re-derives in the update's transaction,
// audited by the update's one row; the old task retires.
func TestDriverUpdateRederivesAttachedEndpoints(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	wrong := strings.Replace(lineProtoSpec, `"^INPUT (\\S+)$"`, `"^INPUT (\\d+)$"`, 1)
	if wrong == lineProtoSpec {
		t.Fatal("fixture regex not found")
	}
	drv, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "panel-proto", Label: "Panel", Version: "1.0.0", Spec: []byte(wrong)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-u"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-u"}, all, all); err != nil {
		t.Fatal(err)
	}
	comp, name, node := "panel-u", "panel-proto", "edge-u"
	ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &name, Component: &comp, Node: &node,
		Inputs: map[string]string{"host": "10.20.4.61"}, SecretRead: all, CanAdmin: true,
	}, all)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	before := endpointTasks(t, ctx, conn, ep.ID)
	var audits int
	countAudits := func() int {
		var n int
		if err := conn.QueryRow(ctx, `select count(*) from audit_log where resource = 'driver' and resource_id = $1`, drv.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	audits = countAudits()

	if _, err := gw.UpdateDriver(ctx, "", "panel-proto", storage.DriverPatch{Spec: []byte(lineProtoSpec)}); err != nil {
		t.Fatal(err)
	}
	after := endpointTasks(t, ctx, conn, ep.ID)
	if len(after) != len(before) {
		t.Fatalf("tasks went from %d to %d; the superseded poll did not retire", len(before), len(after))
	}
	ids := func(rs []taskRow) []string {
		var out []string
		for _, r := range rs {
			out = append(out, r.ID)
		}
		sort.Strings(out)
		return out
	}
	changed := 0
	b, a := ids(before), ids(after)
	for i := range b {
		if b[i] != a[i] {
			changed++
		}
	}
	if changed == 0 {
		t.Fatal("the fixed regex did not re-derive the poll task")
	}
	fixed := false
	for _, r := range after {
		if strings.Contains(r.Spec, `^INPUT (\\S+)$`) && r.Mode == "poll" {
			fixed = true
		}
	}
	if !fixed {
		t.Fatalf("no poll task carries the fixed regex: %+v", after)
	}
	if n := countAudits(); n != audits+1 {
		t.Fatalf("the update wrote %d audit rows, want the one update row", n-audits)
	}
	// The unchanged listener kept its id.
	for _, r := range before {
		if r.Mode == "listen" {
			found := false
			for _, x := range after {
				if x.ID == r.ID {
					found = true
				}
			}
			if !found {
				t.Fatal("an unchanged listener's task id moved")
			}
		}
	}
}

func compactJSON(t *testing.T, raw []byte) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// raceFixture is a custom driver with a digits-only poll regex, a component
// and a node, for the concurrency tests.
func raceFixture(t *testing.T) (context.Context, *storage.PG, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	wrong := strings.Replace(lineProtoSpec, `"^INPUT (\\S+)$"`, `"^INPUT (\\d+)$"`, 1)
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "panel-race", Label: "Panel", Version: "1.0.0", Spec: []byte(wrong)}); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-x"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-x"}, all, all); err != nil {
		t.Fatal(err)
	}
	return ctx, gw, dsn
}

func attachRace(ctx context.Context, gw *storage.PG) (*storage.Endpoint, error) {
	all := scope.Set{All: true}
	comp, name, node := "panel-x", "panel-race", "edge-x"
	return gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &name, Component: &comp, Node: &node,
		Inputs: map[string]string{"host": "10.20.4.80"}, SecretRead: all, CanAdmin: true,
	}, all)
}

// Review of #823: an attach that runs while a driver update is in flight
// waits for it and derives from the new spec. Before, the attach read the
// old committed spec and the update's re-derive could not see the attach's
// uncommitted endpoint, so the endpoint kept the old spec for good.
func TestAttachDuringADriverUpdateDerivesTheNewSpec(t *testing.T) {
	ctx, gw, dsn := raceFixture(t)
	upd, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer upd.Close(ctx)
	tx, err := upd.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The update's row lock, held: the spec now carries the fixed regex.
	if _, err := tx.Exec(ctx, `update driver set spec = $1::jsonb, updated_at = now() where name = 'panel-race'`, lineProtoSpec); err != nil {
		t.Fatal(err)
	}
	type result struct {
		ep  *storage.Endpoint
		err error
	}
	done := make(chan result, 1)
	go func() {
		ep, err := attachRace(ctx, gw)
		done <- result{ep, err}
	}()
	select {
	case r := <-done:
		_ = tx.Rollback(ctx)
		t.Fatalf("the attach did not wait for the driver update in flight (err %v)", r.err)
	case <-time.After(300 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.err != nil {
		t.Fatal(r.err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	for _, task := range endpointTasks(t, ctx, conn, r.ep.ID) {
		if task.Mode == "poll" && strings.Contains(task.Spec, `"driver"`) && !strings.Contains(task.Spec, `^INPUT (\\S+)$`) {
			t.Fatalf("the attach derived from the superseded spec: %s", task.Spec)
		}
	}
}

// Review of #823: a re-derive that meets an endpoint being deleted skips it
// rather than failing the update (or the seed) on the task foreign key.
func TestRederiveSkipsAnEndpointDeletedConcurrently(t *testing.T) {
	ctx, gw, dsn := raceFixture(t)
	ep, err := attachRace(ctx, gw)
	if err != nil {
		t.Fatal(err)
	}
	del, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer del.Close(ctx)
	tx, err := del.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `delete from endpoint where id = $1`, ep.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := gw.UpdateDriver(ctx, "", "panel-race", storage.DriverPatch{Spec: []byte(lineProtoSpec)})
		done <- err
	}()
	time.Sleep(300 * time.Millisecond)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("a concurrent endpoint delete failed the driver update: %v", err)
	}
}
