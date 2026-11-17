package storage_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

func hashHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// TestOutpostGateway proves the outpost enrollment lifecycle and the worklist read:
// create, mint (set token), authenticate, claim, heartbeat, and OutpostWorklist.
// Skipped under -short.
func TestOutpostGateway(t *testing.T) {
	t.Parallel()
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
	all := scope.Set{All: true}

	// Create requires an all-scope grant (outpost is fleet-wide, not tree-scoped).
	if _, err := gw.CreateOutpost(ctx, "", storage.OutpostSpec{Name: "outpost-a", Description: "lab a"}, scope.Set{}, all); !errors.Is(err, storage.ErrOutpostForbidden) {
		t.Fatalf("create without all-scope: want ErrOutpostForbidden, got %v", err)
	}
	n, err := gw.CreateOutpost(ctx, "", storage.OutpostSpec{Name: "outpost-a", Description: "lab a"}, all, all)
	if err != nil {
		t.Fatalf("create outpost: %v", err)
	}
	if n.Name != "outpost-a" || n.Enrolled || n.PrincipalID == "" {
		t.Fatalf("fresh outpost: want name outpost-a, not enrolled, with a principal id, got %+v", n)
	}
	if _, err := gw.CreateOutpost(ctx, "", storage.OutpostSpec{Name: "outpost-a"}, all, all); !errors.Is(err, storage.ErrOutpostExists) {
		t.Fatalf("duplicate outpost: want ErrOutpostExists, got %v", err)
	}

	// The outpost is a first-class principal of kind='outpost' (not a standalone island):
	// its detail row's principal_id points to a principal row of that kind.
	probe, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer probe.Close(ctx)
	var kind string
	if err := probe.QueryRow(ctx, `select kind from principal where id = $1`, n.PrincipalID).Scan(&kind); err != nil {
		t.Fatalf("load outpost principal: %v", err)
	}
	if kind != "outpost" {
		t.Fatalf("outpost principal kind = %q, want outpost", kind)
	}

	// Mint: store the token hash.
	token := "enroll-secret-a"
	if _, err := gw.SetEnrollmentToken(ctx, "", "outpost-a", hashHex(token), all); err != nil {
		t.Fatalf("set enrollment token: %v", err)
	}
	if _, err := gw.SetEnrollmentToken(ctx, "", "ghost", hashHex(token), all); !errors.Is(err, storage.ErrOutpostNotFound) {
		t.Fatalf("mint unknown outpost: want ErrOutpostNotFound, got %v", err)
	}

	// The enrollment secret is a bearer credential ROW on the outpost principal (the
	// same machinery a service token uses), not a bespoke column.
	var credKind string
	if err := probe.QueryRow(ctx,
		`select kind from credential where principal_id = $1`, n.PrincipalID).Scan(&credKind); err != nil {
		t.Fatalf("load outpost credential: %v", err)
	}
	if credKind != "bearer" {
		t.Fatalf("outpost credential kind = %q, want bearer", credKind)
	}

	// Authenticate (the NATS callback path): right hash true, wrong hash false.
	if ok, err := gw.AuthenticateOutpost(ctx, "outpost-a", hashHex(token)); err != nil || !ok {
		t.Fatalf("authenticate right: want true, got (%v,%v)", ok, err)
	}
	if ok, _ := gw.AuthenticateOutpost(ctx, "outpost-a", hashHex("wrong")); ok {
		t.Fatalf("authenticate wrong: want false")
	}
	if ok, _ := gw.AuthenticateOutpost(ctx, "ghost", hashHex(token)); ok {
		t.Fatalf("authenticate unknown outpost: want false")
	}

	// Claim: right token sets enrolled_at; wrong token is rejected.
	claimed, err := gw.ClaimOutpost(ctx, "outpost-a", hashHex(token))
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !claimed.Enrolled || claimed.EnrolledAt == nil {
		t.Fatalf("claimed outpost: want enrolled with enrolled_at, got %+v", claimed)
	}
	if _, err := gw.ClaimOutpost(ctx, "outpost-a", hashHex("wrong")); !errors.Is(err, storage.ErrEnrollmentInvalid) {
		t.Fatalf("claim wrong token: want ErrEnrollmentInvalid, got %v", err)
	}

	// Heartbeat updates last_heartbeat_at.
	if err := gw.RecordHeartbeat(ctx, "outpost-a"); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	got, err := gw.GetOutpost(ctx, "outpost-a", all)
	if err != nil {
		t.Fatalf("get outpost: %v", err)
	}
	if got.LastHeartbeatAt == nil {
		t.Fatalf("last_heartbeat_at not set after heartbeat")
	}

	// Worklist: seed a component + interface + enabled task bound to outpost-a.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `insert into endpoint (name, transport, component, outpost_id, params) values ('disp-1-icmp', 'icmp', (select id from component where name = 'disp-1'), (select principal_id from outpost where name = 'outpost-a'), '{"target":"10.0.0.1"}'::jsonb)`); err != nil {
		t.Fatalf("insert interface: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into task (id, mode, endpoint_id, spec, enabled) values ('t-icmp', 'poll', (select id from endpoint where name = 'disp-1-icmp'), '{"probe":"icmp"}'::jsonb, true)`); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into task (id, mode, endpoint_id, spec, enabled) values ('t-off', 'poll', (select id from endpoint where name = 'disp-1-icmp'), '{}'::jsonb, false)`); err != nil {
		t.Fatalf("insert disabled task: %v", err)
	}

	wl, err := gw.OutpostWorklist(ctx, "outpost-a")
	if err != nil {
		t.Fatalf("worklist: %v", err)
	}
	if len(wl.Tasks) != 1 {
		t.Fatalf("worklist tasks = %d, want 1 (enabled only)", len(wl.Tasks))
	}
	if wl.Tasks[0].ID != "t-icmp" || wl.Tasks[0].Transport != "icmp" || wl.Tasks[0].Mode != "poll" {
		t.Fatalf("worklist task: got %+v", wl.Tasks[0])
	}
	// The endpoint id rides with each task: endpoint names are unique only
	// per component, and an outpost keys a held session by the endpoint (#816).
	var epID string
	if err := conn.QueryRow(ctx, `select id::text from endpoint where name = 'disp-1-icmp'`).Scan(&epID); err != nil {
		t.Fatalf("endpoint id: %v", err)
	}
	if wl.Tasks[0].EndpointID != epID {
		t.Fatalf("worklist task endpoint id = %q, want %q", wl.Tasks[0].EndpointID, epID)
	}
	if wl.ConfigGeneration == 0 {
		t.Fatalf("config_generation = 0, want the interface's updated_at epoch")
	}

	// An unknown outpost worklist is empty, not an error.
	empty, err := gw.OutpostWorklist(ctx, "no-such-outpost")
	if err != nil {
		t.Fatalf("worklist unknown outpost: %v", err)
	}
	if len(empty.Tasks) != 0 || empty.ConfigGeneration != 0 {
		t.Fatalf("unknown outpost worklist: want empty/0, got %+v", empty)
	}
}

// TestOutpostNameSubjectSafety proves the outpost-name invariant is enforced at the
// Storage Gateway: an outpost name becomes a NATS subject token
// (og.v1.telemetry.<name>, ...) and its per-outpost subject grant, so a name
// containing a dot, a subject wildcard ('*', '>'), or whitespace must be refused
// with ErrInvalidEntityName before any row is written. The entity name rule
// (storage.ValidateName) is the only fence since #570 dropped the outpost table's
// own CHECK, which forbade exactly these characters and no others; this is the
// test that keeps the guarantee the CHECK used to state. Skipped under -short.
func TestOutpostNameSubjectSafety(t *testing.T) {
	t.Parallel()
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
	all := scope.Set{All: true}

	// Every character the dropped CHECK caught (a dot, '*', '>', a space, a tab,
	// a newline, a carriage return), plus two it admitted that the entity rule
	// refuses (uppercase, an underscore): one rule, refusing all of them.
	for _, bad := range []string{"bad.name", "edge*", "edge>", "has space", "tab\tname", "new\nline", "carriage\rreturn", "Edge-1", "edge_1"} {
		if _, err := gw.CreateOutpost(ctx, "", storage.OutpostSpec{Name: bad, Description: "subject-unsafe"}, all, all); !errors.Is(err, storage.ErrInvalidEntityName) {
			t.Fatalf("create outpost %q: want ErrInvalidEntityName, got %v", bad, err)
		}
		// "bad.name" is dot-joined, so #627 Task 12's RejectAddressForm now
		// catches it before the lookup even runs and reports the more specific
		// ErrAddressNotAccepted ("an outpost name is a single token") rather than
		// blending into the generic not-found every other subject-unsafe name
		// still gets: the row was never written either way, but a caller gets
		// told WHY only when the ref looked like #627's address grammar.
		_, err := gw.GetOutpost(ctx, bad, all)
		if bad == "bad.name" {
			var notAccepted *storage.ErrAddressNotAccepted
			if !errors.As(err, &notAccepted) {
				t.Fatalf("get outpost %q after rejected create: want *storage.ErrAddressNotAccepted, got %v", bad, err)
			}
			continue
		}
		if !errors.Is(err, storage.ErrOutpostNotFound) {
			t.Fatalf("get outpost %q after rejected create: want ErrOutpostNotFound (no row), got %v", bad, err)
		}
	}

	// A subject-safe name still succeeds.
	if _, err := gw.CreateOutpost(ctx, "", storage.OutpostSpec{Name: "outpost-safe", Description: "ok"}, all, all); err != nil {
		t.Fatalf("create valid outpost: %v", err)
	}
}

// TestGetOutpostRefusesADottedAddress is the storage-layer half of #627 Task
// 12's 22-catalog refusal: an outpost's name stays a single global kebab token
// (it rides NATS subjects), so a ref that looks like the tree-entity address
// grammar (dot-joined, or opening on an accessor) must be the explicit
// *storage.ErrAddressNotAccepted, naming the kind, never a fall-through
// ErrOutpostNotFound that looks identical to an ordinary unknown outpost.
func TestGetOutpostRefusesADottedAddress(t *testing.T) {
	t.Parallel()
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
	all := scope.Set{All: true}

	for _, ref := range []string{"boi.17c", "$comp.display-1"} {
		_, err := gw.GetOutpost(ctx, ref, all)
		var notAccepted *storage.ErrAddressNotAccepted
		if !errors.As(err, &notAccepted) {
			t.Fatalf("GetOutpost(%q) = %v, want *storage.ErrAddressNotAccepted", ref, err)
		}
		if notAccepted.Kind != "outpost" {
			t.Errorf("GetOutpost(%q) Kind = %q, want %q", ref, notAccepted.Kind, "outpost")
		}
	}
}

// TestOutpostIdentityAndEdit covers the N1 identity fields (label, location)
// and the UpdateOutpost patch path: nil-unchanged semantics, location set / change /
// clear, an unknown location rejected, name immutability, and the location FK's
// ON DELETE SET NULL. See .claude/superpowers/specs/2026-07-19-outpost-identity-and-edit-design.md.
func TestOutpostIdentityAndEdit(t *testing.T) {
	t.Parallel()
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
	all := scope.Set{All: true}

	closet, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq-closet", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	other, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "east-rack", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("create other location: %v", err)
	}

	// Create carries label and location.
	n, err := gw.CreateOutpost(ctx, "", storage.OutpostSpec{
		Name: "edge-hq", Label: "HQ Closet Outpost", Description: "rack 3", LocationName: &closet.Name,
	}, all, all)
	if err != nil {
		t.Fatalf("create outpost: %v", err)
	}
	if n.Label != "HQ Closet Outpost" || n.LocationName == nil || *n.LocationName != "hq-closet" {
		t.Fatalf("fresh outpost identity: got display=%q location=%v", n.Label, n.LocationName)
	}

	str := func(s string) *string { return &s }

	// Patch label only: description and location are untouched (nil = unchanged).
	up, err := gw.UpdateOutpost(ctx, "", "edge-hq", storage.OutpostPatch{Label: str("HQ Outpost")}, all, all, all)
	if err != nil {
		t.Fatalf("update label: %v", err)
	}
	if up.Label != "HQ Outpost" || up.Description != "rack 3" || up.LocationName == nil || *up.LocationName != "hq-closet" {
		t.Fatalf("patch display only: got %+v", up)
	}
	if up.Name != "edge-hq" {
		t.Fatalf("name must be immutable, got %q", up.Name)
	}

	// Move the location to another valid one.
	up, err = gw.UpdateOutpost(ctx, "", "edge-hq", storage.OutpostPatch{LocationName: &other.Name}, all, all, all)
	if err != nil {
		t.Fatalf("relocate: %v", err)
	}
	if up.LocationName == nil || *up.LocationName != "east-rack" {
		t.Fatalf("relocate: got %v", up.LocationName)
	}

	// Clear the location (a pointer to "").
	up, err = gw.UpdateOutpost(ctx, "", "edge-hq", storage.OutpostPatch{LocationName: str("")}, all, all, all)
	if err != nil {
		t.Fatalf("clear location: %v", err)
	}
	if up.LocationName != nil {
		t.Fatalf("clear location: want nil, got %v", *up.LocationName)
	}

	// An unknown location is rejected (the location FK), not silently applied.
	if _, err := gw.UpdateOutpost(ctx, "", "edge-hq", storage.OutpostPatch{LocationName: str("ghost-room")}, all, all, all); !errors.Is(err, storage.ErrLocationNotFound) {
		t.Fatalf("unknown location: want ErrLocationNotFound, got %v", err)
	}

	// Fleet-wide: an update without all-scope is forbidden; an unknown outpost is not found.
	if _, err := gw.UpdateOutpost(ctx, "", "edge-hq", storage.OutpostPatch{Label: str("x")}, scope.Set{}, scope.Set{}, all); !errors.Is(err, storage.ErrOutpostForbidden) {
		t.Fatalf("update without all-scope: want ErrOutpostForbidden, got %v", err)
	}
	if _, err := gw.UpdateOutpost(ctx, "", "ghost", storage.OutpostPatch{Label: str("x")}, all, all, all); !errors.Is(err, storage.ErrOutpostNotFound) {
		t.Fatalf("update unknown outpost: want ErrOutpostNotFound, got %v", err)
	}

	// The location FK is ON DELETE SET NULL: place the outpost, delete its location,
	// the outpost survives with a cleared placement (deleted via a probe, since
	// location-delete guards are not the unit under test).
	if _, err := gw.UpdateOutpost(ctx, "", "edge-hq", storage.OutpostPatch{LocationName: &closet.Name}, all, all, all); err != nil {
		t.Fatalf("re-place before delete: %v", err)
	}
	probe, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer probe.Close(ctx)
	if _, err := probe.Exec(ctx, `delete from location where name = 'hq-closet'`); err != nil {
		t.Fatalf("delete location: %v", err)
	}
	got, err := gw.GetOutpost(ctx, "edge-hq", all)
	if err != nil {
		t.Fatalf("get outpost after location delete: %v", err)
	}
	if got.LocationName != nil {
		t.Fatalf("ON DELETE SET NULL: want nil location, got %v", *got.LocationName)
	}
}

// TestDeleteOutpost covers outpost decommission (N3): a hard delete of the outpost
// principal cascades its interfaces, their derived tasks, and its outpost-owned tag
// bindings, while a component's own telemetry (owner arc = component, outpost_id
// null) survives. Plus the all-scope gate and the unknown-outpost not-found.
func TestDeleteOutpost(t *testing.T) {
	t.Parallel()
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
	all := scope.Set{All: true}

	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "dsp-1"}, all, all, all, all); err != nil {
		t.Fatalf("component: %v", err)
	}
	outpost, err := gw.CreateOutpost(ctx, "", storage.OutpostSpec{Name: "edge-del"}, all, all)
	if err != nil {
		t.Fatalf("create outpost: %v", err)
	}
	comp, outpostName := "dsp-1", "edge-del"
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Transport: "tcp", Component: &comp, Outpost: &outpostName, Params: []byte(`{"target":"10.0.0.1:80"}`),
	}, all); err != nil {
		t.Fatalf("create interface: %v", err)
	}
	// A component-owned sample (owner arc = component, outpost_id null): it must
	// survive the outpost's deletion.
	if err := insertMetrics(ctx, gw, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: "dsp-1", Key: "tcp-connect-time", Instance: "tcp", Value: 1, Source: "tcp", TS: time.Now().UTC()},
	}); err != nil {
		t.Fatalf("insert component sample: %v", err)
	}
	// An outpost-owned tag binding (cascades on delete).
	mustOutpostTag(t, gw, outpost.Name)

	probe, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("probe connect: %v", err)
	}
	defer probe.Close(ctx)
	count := func(q string, args ...any) int {
		var n int
		if err := probe.QueryRow(ctx, q, args...).Scan(&n); err != nil {
			t.Fatalf("count (%s): %v", q, err)
		}
		return n
	}
	if count(`select count(*) from endpoint where outpost_id = (select principal_id from outpost where name = $1)`, outpostName) != 1 {
		t.Fatalf("precondition: want 1 interface on the outpost")
	}
	if count(`select count(*) from task`) != 1 {
		t.Fatalf("precondition: want 1 derived task")
	}
	if count(`select count(*) from tag_binding where outpost_id = $1`, outpost.PrincipalID) != 1 {
		t.Fatalf("precondition: want 1 outpost tag binding")
	}

	// The all-scope gate and unknown-outpost, then the delete.
	if err := gw.DeleteOutpost(ctx, "", outpostName, scope.Set{}, scope.Set{}); !errors.Is(err, storage.ErrOutpostForbidden) {
		t.Fatalf("delete without all-scope: want ErrOutpostForbidden, got %v", err)
	}
	if err := gw.DeleteOutpost(ctx, "", "ghost", all, all); !errors.Is(err, storage.ErrOutpostNotFound) {
		t.Fatalf("delete unknown outpost: want ErrOutpostNotFound, got %v", err)
	}
	if err := gw.DeleteOutpost(ctx, "", outpostName, all, all); err != nil {
		t.Fatalf("delete outpost: %v", err)
	}

	// The outpost and everything keyed to it are gone; the outpost principal too.
	if _, err := gw.GetOutpost(ctx, outpostName, all); !errors.Is(err, storage.ErrOutpostNotFound) {
		t.Errorf("outpost still present after delete: %v", err)
	}
	if n := count(`select count(*) from outpost where name = $1`, outpostName); n != 0 {
		t.Errorf("outpost rows = %d, want 0", n)
	}
	if n := count(`select count(*) from principal where id = $1`, outpost.PrincipalID); n != 0 {
		t.Errorf("outpost principal rows = %d, want 0", n)
	}
	if n := count(`select count(*) from endpoint where outpost_id = (select principal_id from outpost where name = $1)`, outpostName); n != 0 {
		t.Errorf("interfaces = %d, want 0 (cascade)", n)
	}
	if n := count(`select count(*) from task`); n != 0 {
		t.Errorf("tasks = %d, want 0 (cascade through the interface)", n)
	}
	if n := count(`select count(*) from tag_binding where outpost_id = $1`, outpost.PrincipalID); n != 0 {
		t.Errorf("outpost tag bindings = %d, want 0 (cascade)", n)
	}
	// The component's own telemetry survives (owner arc = component, not the outpost).
	if n := count(`select count(*) from ` + storagetest.MetricRows + ` metric where owner_kind = 'component'`); n != 1 {
		t.Errorf("component samples = %d, want 1 (must survive the outpost delete)", n)
	}
}

// mustOutpostTag mints an outpost-applicable key and binds it to the outpost.
func mustOutpostTag(t *testing.T, gw storage.Gateway, outpostName string) {
	t.Helper()
	ctx := context.Background()
	all := scope.Set{All: true}
	if _, err := gw.CreateTag(ctx, "", storage.TagSpec{Name: "rack", AppliesTo: []string{"outpost"}}, all); err != nil {
		t.Fatalf("create tag: %v", err)
	}
	if _, err := gw.SetTagBinding(ctx, "", "rack", "outpost", &outpostName, "r5", all, all); err != nil {
		t.Fatalf("bind outpost tag: %v", err)
	}
}
