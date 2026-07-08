package devseed_test

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// officialRoles are the role ids the boot seed installs; every fixture grant must
// name one of them, else the grant's role_id foreign key fails at seed time.
var officialRoles = map[string]bool{
	"viewer": true, "operator": true, "deploy": true, "admin": true, "owner": true,
}

// TestFixturesShape is a pure unit check on the embedded fixtures: the tree is
// well formed (every parent named before its children), every user carries a
// password, and every grant references a real role and (when scoped) a location
// declared in the same document. It needs no database, so it runs under -short.
func TestFixturesShape(t *testing.T) {
	doc, err := devseed.Fixtures()
	if err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}
	if len(doc.Locations) == 0 || len(doc.Users) == 0 {
		t.Fatalf("fixtures empty: %d locations, %d users", len(doc.Locations), len(doc.Users))
	}

	seenLoc := map[string]bool{}
	for _, l := range doc.Locations {
		if l.Name == "" || l.Type == "" {
			t.Errorf("location %+v missing name or type", l)
		}
		if l.Parent != "" && !seenLoc[l.Parent] {
			t.Errorf("location %q references parent %q not declared before it", l.Name, l.Parent)
		}
		seenLoc[l.Name] = true
	}

	for _, u := range doc.Users {
		if u.Username == "" || u.Password == "" {
			t.Errorf("user %+v missing username or password", u)
		}
		if len(u.Grants) == 0 {
			t.Errorf("user %q has no grants (a dev user without access is not useful)", u.Username)
		}
		for _, g := range u.Grants {
			if !officialRoles[g.Role] {
				t.Errorf("user %q grant references unknown role %q", u.Username, g.Role)
			}
			if g.ScopeKind != "all" && !seenLoc[g.ScopeRef] {
				t.Errorf("user %q grant scoped to location %q not in the fixtures", u.Username, g.ScopeRef)
			}
		}
	}
}

// TestRunIdempotent proves devseed.Run lands the example estate (and the worked
// reachability check) through the Storage Gateway and that a second run neither
// duplicates nor errors: make dev runs it on every start. Reference data (roles,
// location types, datapoint types) must exist first, so the boot seed runs ahead of
// it, exactly as bootstrap does. Skipped under -short by the testcontainer harness.
func TestRunIdempotent(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()

	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()

	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("boot seed: %v", err)
	}
	// Run twice: idempotency is the property under test.
	for i := 0; i < 2; i++ {
		if err := devseed.Run(ctx, gw, ""); err != nil {
			t.Fatalf("devseed run %d: %v", i, err)
		}
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	// Counts prove idempotency: the second Run added nothing.
	var locs, humans, grants int
	if err := conn.QueryRow(ctx, `select count(*) from location`).Scan(&locs); err != nil {
		t.Fatalf("count locations: %v", err)
	}
	if locs != 13 {
		t.Errorf("locations = %d, want 13 (seed not idempotent or incomplete)", locs)
	}
	// A multi-site estate: three campuses, not one.
	var campuses int
	if err := conn.QueryRow(ctx, `select count(*) from location where location_type = 'campus'`).Scan(&campuses); err != nil {
		t.Fatalf("count campuses: %v", err)
	}
	if campuses != 3 {
		t.Errorf("campuses = %d, want 3 (hq, east, airport)", campuses)
	}
	if err := conn.QueryRow(ctx, `select count(*) from principal where kind = 'human'`).Scan(&humans); err != nil {
		t.Fatalf("count humans: %v", err)
	}
	if humans != 3 {
		t.Errorf("human principals = %d, want 3", humans)
	}
	if err := conn.QueryRow(ctx, `select count(*) from principal_grant`).Scan(&grants); err != nil {
		t.Fatalf("count grants: %v", err)
	}
	if grants != 3 {
		t.Errorf("grants = %d, want 3", grants)
	}

	// The tree links resolve: the west building hangs under the hq campus.
	var parentName string
	if err := conn.QueryRow(ctx, `
		select p.name from location c join location p on p.id = c.parent_id
		where c.name = 'hq-west'`).Scan(&parentName); err != nil {
		t.Fatalf("read hq-west parent: %v", err)
	}
	if parentName != "hq" {
		t.Errorf("hq-west parent = %q, want hq", parentName)
	}

	// Each seeded user has a password credential (so they can sign in to make dev).
	var pwCreds int
	if err := conn.QueryRow(ctx, `
		select count(*) from credential
		where kind = 'password'
		  and principal_id in (select principal_id from human
		    where username in ('operator', 'viewer-hq', 'tech-east'))`).Scan(&pwCreds); err != nil {
		t.Fatalf("count password creds: %v", err)
	}
	if pwCreds != 3 {
		t.Errorf("password credentials for seeded users = %d, want 3", pwCreds)
	}

	// Grant shapes: operator is all-scoped; viewer-hq reads the hq subtree; tech-east
	// deploys under the East campus excluding its root. The region-scoped users name
	// their region; the all-scoped one does not.
	assertGrant(t, conn, ctx, "operator", "operator", "all", "", "subtree")
	assertGrant(t, conn, ctx, "viewer-hq", "viewer", "location", "hq", "subtree")
	assertGrant(t, conn, ctx, "tech-east", "deploy", "location", "east", "subtree_excl_root")

	// The worked reachability check: an enrolled node, a tcp interface owned by the
	// boardroom-display component and placed on the node, a poll task over it, and the
	// datapoints the panel reads. Every count is over two Runs, so a duplicate here is
	// the seed failing to be idempotent.
	all := scope.Set{All: true}

	// The component the check hangs on, placed under the HQ boardroom.
	var comps int
	if err := conn.QueryRow(ctx, `select count(*) from component where name = 'hq-boardroom-display'`).Scan(&comps); err != nil {
		t.Fatalf("count reachability component: %v", err)
	}
	if comps != 1 {
		t.Errorf("reachability component rows = %d, want 1 (seed not idempotent)", comps)
	}

	// The node is created, enrolled, and claimed, so it reads as enrolled.
	node, err := gw.GetNode(ctx, "edge-hq", all)
	if err != nil {
		t.Fatalf("get seeded node: %v", err)
	}
	if !node.Enrolled {
		t.Errorf("node edge-hq enrolled = false, want true (created, enrolled, and claimed)")
	}

	// The interface: type tcp, owned by the component, placed on the node, one row.
	// Interfaces are id-keyed now, so resolve the friendly name (unique on its
	// component) through the scoped list, then keep its id for the task check.
	var iface *storage.Interface
	ifaces, err := gw.ListInterfaces(ctx, all)
	if err != nil {
		t.Fatalf("list interfaces: %v", err)
	}
	for i := range ifaces {
		if ifaces[i].Name == "boardroom-tcp" {
			iface = &ifaces[i]
		}
	}
	if iface == nil {
		t.Fatalf("seeded interface boardroom-tcp not found")
	}
	if iface.Type != "tcp" || iface.Component == nil || *iface.Component != "hq-boardroom-display" || iface.Node == nil || *iface.Node != "edge-hq" {
		t.Errorf("seeded interface = %+v, want tcp on component hq-boardroom-display / node edge-hq", iface)
	}
	var ifaceCount int
	if err := conn.QueryRow(ctx, `select count(*) from interface where name = 'boardroom-tcp'`).Scan(&ifaceCount); err != nil {
		t.Fatalf("count reachability interface: %v", err)
	}
	if ifaceCount != 1 {
		t.Errorf("reachability interface rows = %d, want 1 (seed not idempotent)", ifaceCount)
	}

	// Exactly one poll task over the interface (referenced by its surrogate id).
	tasks, err := gw.ListTasks(ctx, all)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	var reachTasks int
	for i := range tasks {
		if tasks[i].InterfaceID == iface.ID {
			reachTasks++
			if tasks[i].Mode != "poll" || !tasks[i].Enabled {
				t.Errorf("reachability task = %+v, want mode poll enabled", tasks[i])
			}
		}
	}
	if reachTasks != 1 {
		t.Errorf("reachability task rows = %d, want 1 (seed not idempotent)", reachTasks)
	}

	// The datapoints populate the reachability read the panel composes: a fresh "up"
	// verdict, an up->down->up transition sequence (a healthy baseline, a brief
	// outage, a recovery) that renders as a mostly-up availability strip with a thin
	// down blip, and both probe layers green. The transition count proves the
	// datapoints did not double on the second Run (they are append-only, so the
	// sentinel must have skipped them).
	verdict, err := gw.LatestState(ctx, "hq-boardroom-display", "interface.reachable", "boardroom-tcp")
	if err != nil {
		t.Fatalf("latest verdict: %v", err)
	}
	if verdict == nil || verdict.Value != "up" {
		t.Fatalf("seeded verdict = %+v, want value up", verdict)
	}
	transitions, err := gw.StateTransitions(ctx, "hq-boardroom-display", "interface.reachable", "boardroom-tcp", time.Time{})
	if err != nil {
		t.Fatalf("state transitions: %v", err)
	}
	if len(transitions) != 3 {
		t.Errorf("verdict transitions = %d, want 3 (up->down->up), idempotent across two Runs", len(transitions))
	}
	tcpOpen, err := gw.LatestMetricInstance(ctx, "hq-boardroom-display", "tcp.open", "boardroom-tcp")
	if err != nil {
		t.Fatalf("latest tcp.open: %v", err)
	}
	if tcpOpen == nil || tcpOpen.Value != 1 {
		t.Errorf("seeded tcp.open = %+v, want 1", tcpOpen)
	}
	icmpReach, err := gw.LatestMetricInstance(ctx, "hq-boardroom-display", "icmp.reachable", "boardroom-tcp")
	if err != nil {
		t.Fatalf("latest icmp.reachable: %v", err)
	}
	if icmpReach == nil || icmpReach.Value != 1 {
		t.Errorf("seeded icmp.reachable = %+v, want 1", icmpReach)
	}
}

// assertGrant checks a seeded user holds exactly the expected role at the
// expected scope. scopeName is the location name the grant points at ("" for the
// all scope), resolved to its id for the comparison.
func assertGrant(t *testing.T, conn *pgx.Conn, ctx context.Context, username, role, scopeKind, scopeName, scopeOp string) {
	t.Helper()
	var gotKind, gotOp string
	var gotScopeID *string
	if err := conn.QueryRow(ctx, `
		select g.scope_kind, g.scope_op, g.scope_id
		from principal_grant g join human h on h.principal_id = g.principal_id
		where h.username = $1 and g.role_id = $2`, username, role).Scan(&gotKind, &gotOp, &gotScopeID); err != nil {
		t.Fatalf("read grant for %s/%s: %v", username, role, err)
	}
	if gotKind != scopeKind || gotOp != scopeOp {
		t.Errorf("%s grant = kind %q op %q, want kind %q op %q", username, gotKind, gotOp, scopeKind, scopeOp)
	}
	if scopeName == "" {
		if gotScopeID != nil {
			t.Errorf("%s all-scope grant has scope_id %v, want null", username, *gotScopeID)
		}
		return
	}
	var wantID string
	if err := conn.QueryRow(ctx, `select id from location where name = $1`, scopeName).Scan(&wantID); err != nil {
		t.Fatalf("resolve location %q: %v", scopeName, err)
	}
	if gotScopeID == nil || *gotScopeID != wantID {
		t.Errorf("%s grant scope_id = %v, want %s (%s)", username, gotScopeID, wantID, scopeName)
	}
}
