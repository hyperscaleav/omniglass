package storage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// newDuplicateNameFixture opens a gateway on a throwaway database with the
// #627 migration already applied, so two components (or systems, or
// locations) can legitimately share a name as long as they land in
// different placement buckets, exactly the way every fixture below builds
// them (twoDisplayOnes places its two "display-1" components in different
// rooms; twoSameNamedSystems places its two "huddle-1" systems at different
// locations).
//
// This used to drop component_name_key/system_name_key/location_name_key by
// raw DDL, because the migration that relaxes those constraints for real was
// a later task and this fixture only needed to prove the GATEWAY's own
// queries survive that world, not implement it. That DDL now runs for real
// (db/migrations/20260808090000_names_scope_to_placement.sql), and the three
// constraints it drops no longer exist to drop a second time here, so the
// hack is gone: NewPG on a freshly migrated database already gives every
// fixture below the world it was built for.
func newDuplicateNameFixture(t *testing.T) (gw storage.Gateway, conn *pgx.Conn) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })

	gw, err = storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return gw, conn
}

// twoDisplayOnes creates two components both named "display-1", in different
// rooms, plus a system and a quorum-1 role to staff. It is the shared fixture
// TestDuplicateNamesDoNotBreakGatewayReads and TestBareNameAmbiguousRefuses both
// build on.
func twoDisplayOnes(t *testing.T, gw storage.Gateway) (compA, compB *storage.Component, sys *storage.System) {
	t.Helper()
	ctx := context.Background()
	all := scope.Set{All: true}

	roomA, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "room-a", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("room-a: %v", err)
	}
	roomB, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "room-b", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("room-b: %v", err)
	}
	compA, err = gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "display-1", LocationName: strptr(roomA.Name)}, all, all, all, all)
	if err != nil {
		t.Fatalf("component in room-a: %v", err)
	}
	compB, err = gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "display-1", LocationName: strptr(roomB.Name)}, all, all, all, all)
	if err != nil {
		t.Fatalf("component in room-b: %v", err)
	}
	if compA.ID == compB.ID {
		t.Fatalf("the two components did not actually land on different rows")
	}

	sys, err = gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "duptest-sys"}, all, all)
	if err != nil {
		t.Fatalf("system: %v", err)
	}
	if _, err := gw.SetSystemRole(ctx, "", "system", sys.Name, storage.SystemRoleSpec{
		Name: "seat", DisplayName: "Seat", Quorum: 1,
	}); err != nil {
		t.Fatalf("declare role: %v", err)
	}
	return compA, compB, sys
}

// twoSameNamedSystems creates two systems both named "huddle-1", at different
// locations, so component/location fixtures are not the only shared name in
// play: the collection ingest path and role_declarations.go's roleOwnerExpr
// key on system, not component.
func twoSameNamedSystems(t *testing.T, gw storage.Gateway) (sysA, sysB *storage.System) {
	t.Helper()
	ctx := context.Background()
	all := scope.Set{All: true}

	locA, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "sys-room-a", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("sys-room-a: %v", err)
	}
	locB, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "sys-room-b", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("sys-room-b: %v", err)
	}
	sysA, err = gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "huddle-1", LocationName: strptr(locA.Name)}, all, all)
	if err != nil {
		t.Fatalf("system in sys-room-a: %v", err)
	}
	sysB, err = gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "huddle-1", LocationName: strptr(locB.Name)}, all, all)
	if err != nil {
		t.Fatalf("system in sys-room-b: %v", err)
	}
	if sysA.ID == sysB.ID {
		t.Fatalf("the two systems did not actually land on different rows")
	}
	return sysA, sysB
}

// TestDuplicateNamesDoNotBreakIngestPath covers the collection ingest and
// command-settlement paths TestDuplicateNamesDoNotBreakGatewayReads does not
// reach: property_samples.go's InsertPropertySamples (the hot write path:
// every telemetry batch for a component takes it), settlement.go's
// settleCheck and latestMetricValue (reached through IssueCommand and
// CommandSettlement), and current_values.go's LatestValue. All four build
// their SQL through the shared, owner-kind-generic ownerArcExprN, which
// resolved a component/system/location owner by an inline name subquery; that
// primitive is also used by health.go (already covered) and by callers this
// test exercises directly. property_values.go's EffectiveProperties and
// metric_values.go's EffectiveMetrics are covered too: their owner lookup is
// a CTE, not a scalar subquery, so an ambiguous name there does not raise
// SQLSTATE 21000 at all, it silently unions rows from every same-named owner
// into one answer, a worse failure this task's fix (binding the resolved id
// instead of a name) also closes.
func TestDuplicateNamesDoNotBreakIngestPath(t *testing.T) {
	gw, conn := newDuplicateNameFixture(t)
	ctx := context.Background()
	all := scope.Set{All: true}
	compA, compB, _ := twoDisplayOnes(t, gw)

	if _, err := gw.CreatePropertyType(ctx, "", storage.PropertyTypeSpec{Name: "video-input-x", DataType: "string"}); err != nil {
		t.Fatalf("create property type: %v", err)
	}
	if _, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{Name: "volume-x", DataType: "float"}); err != nil {
		t.Fatalf("create metric type: %v", err)
	}

	// The ingest write path, by uuid: InsertPropertySamples must not 21000 and
	// must land on the right row (property_samples.go).
	if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{
		OwnerKind: "component", OwnerID: compA.ID, Key: "video-input-x", Value: "hdmi2", Source: "test", TS: time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("insert property sample on compA by id: %v", err)
	}
	var landedComponent string
	if err := conn.QueryRow(ctx,
		`select component_id from property where property_type_id = (select id from property_type where name = 'video-input-x') and provenance = 'observed'`,
	).Scan(&landedComponent); err != nil {
		t.Fatalf("read landed property sample: %v", err)
	}
	if landedComponent != compA.ID {
		t.Fatalf("property sample landed on %s, want %s", landedComponent, compA.ID)
	}

	// A declared value, by uuid, then read back through LatestValue
	// (current_values.go's public entry over latestValue) and
	// EffectiveProperties (property_values.go): both must not 21000, and
	// compB's read of the same series must come back empty, not compA's value.
	if _, err := gw.SetProperty(ctx, "", "component", compA.ID, "video-input-x", "", []byte(`"hdmi1"`), all); err != nil {
		t.Fatalf("declare property on compA by id: %v", err)
	}
	cur, err := gw.LatestValue(ctx, "component", compA.ID, "video-input-x", "", "declared", all)
	if err != nil {
		t.Fatalf("latest value on compA by id: %v", err)
	}
	if cur == nil || string(cur.Value) != `"hdmi1"` {
		t.Fatalf("latest value on compA = %+v, want hdmi1", cur)
	}
	curB, err := gw.LatestValue(ctx, "component", compB.ID, "video-input-x", "", "declared", all)
	if err != nil {
		t.Fatalf("latest value on compB by id: %v", err)
	}
	if curB != nil {
		t.Fatalf("latest value on compB = %+v, want nil (cross-contaminated by the shared name)", curB)
	}
	effA, err := gw.EffectiveProperties(ctx, "component", compA.ID, all)
	if err != nil {
		t.Fatalf("effective properties on compA by id: %v", err)
	}
	if !anyEffectiveProperty(effA, "video-input-x", `"hdmi1"`) {
		t.Fatalf("effective properties on compA = %+v, want video-input-x = hdmi1", effA)
	}
	effB, err := gw.EffectiveProperties(ctx, "component", compB.ID, all)
	if err != nil {
		t.Fatalf("effective properties on compB by id: %v", err)
	}
	if anyEffectiveProperty(effB, "video-input-x", `"hdmi1"`) {
		t.Fatalf("effective properties on compB = %+v, leaked compA's declared value (cross-contaminated by the shared name)", effB)
	}

	// A metric sample, by uuid, then read back through EffectiveMetrics
	// (metric_values.go, the CTE twin of EffectiveProperties): same shape.
	if err := gw.InsertMetricSamples(ctx, []storage.MetricSampleWrite{{
		OwnerKind: "component", OwnerID: compA.ID, Key: "volume-x", Value: 42, Source: "test", TS: time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("insert metric sample on compA by id: %v", err)
	}
	metA, err := gw.EffectiveMetrics(ctx, "component", compA.ID, all)
	if err != nil {
		t.Fatalf("effective metrics on compA by id: %v", err)
	}
	if !anyEffectiveMetricSampled(metA, "volume-x") {
		t.Fatalf("effective metrics on compA = %+v, want volume-x sampled", metA)
	}
	metB, err := gw.EffectiveMetrics(ctx, "component", compB.ID, all)
	if err != nil {
		t.Fatalf("effective metrics on compB by id: %v", err)
	}
	if anyEffectiveMetricSampled(metB, "volume-x") {
		t.Fatalf("effective metrics on compB = %+v, leaked compA's sample (cross-contaminated by the shared name)", metB)
	}

	// A metric settlement, by uuid: IssueCommand opens an intended value and
	// runs the settle-check in the same transaction (settleCheck,
	// latestTargetValue -> latestMetricValue); CommandSettlement re-derives
	// the verdict. Both must not 21000 and must settle against compA's own
	// observed sample, not compB's (which has none).
	if _, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{
		Name: "set-volume-x", TargetMetricType: "volume-x", SettleWindowSeconds: 0,
	}); err != nil {
		t.Fatalf("create command type: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: make([]byte, 32), Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap owner: %v", err)
	}
	var actor string
	if err := conn.QueryRow(ctx, `select principal_id from human where username = 'root'`).Scan(&actor); err != nil {
		t.Fatalf("resolve actor: %v", err)
	}
	cmd, err := gw.IssueCommand(ctx, actor, "component", compA.ID, "set-volume-x", "", []byte(`42`), nil, all)
	if err != nil {
		t.Fatalf("issue command on compA by id: %v", err)
	}
	var status string
	if err := conn.QueryRow(ctx, `select status from command where id = $1`, cmd.ID).Scan(&status); err != nil {
		t.Fatalf("read command status: %v", err)
	}
	if status != "settled" {
		t.Fatalf("command status = %q, want settled (matches compA's own observed sample)", status)
	}
	verdict, err := gw.CommandSettlement(ctx, "component", compA.ID, "set-volume-x", "", all)
	if err != nil {
		t.Fatalf("command settlement on compA by id: %v", err)
	}
	if verdict != storage.SettlementSettled {
		t.Fatalf("command settlement = %q, want settled", verdict)
	}
}

func anyEffectiveProperty(effs []storage.EffectiveProperty, name, value string) bool {
	for _, e := range effs {
		if e.PropertyTypeName == name && string(e.Value) == value {
			return true
		}
	}
	return false
}

func anyEffectiveMetricSampled(mets []storage.EffectiveMetric, name string) bool {
	for _, m := range mets {
		if m.MetricTypeName == name && m.IsSampled {
			return true
		}
	}
	return false
}

// TestSystemOwnedRoleDeclarationSurvivesDuplicateNames covers
// role_declarations.go's roleOwnerExpr, the system branch of the role-owner
// arc: SetSystemRole/ListSystemRoles/DeleteSystemRole must resolve a
// system-owner reference once and bind its uuid rather than re-deriving one
// from a name a second system might now share, and roleOwnerArg's
// not-found-falls-through-to-the-CHECK-constraint behavior must stay intact
// for an owner that genuinely does not exist.
func TestSystemOwnedRoleDeclarationSurvivesDuplicateNames(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	ctx := context.Background()
	sysA, sysB := twoSameNamedSystems(t, gw)

	if _, err := gw.SetSystemRole(ctx, "", "system", sysA.ID, storage.SystemRoleSpec{
		Name: "seat", DisplayName: "Seat", Quorum: 1,
	}); err != nil {
		t.Fatalf("declare role on sysA by id: %v", err)
	}

	rolesA, err := gw.ListSystemRoles(ctx, "system", sysA.ID)
	if err != nil {
		t.Fatalf("list roles on sysA by id: %v", err)
	}
	if len(rolesA) != 1 || rolesA[0].Name != "seat" {
		t.Fatalf("sysA roles = %+v, want one role named seat", rolesA)
	}
	rolesB, err := gw.ListSystemRoles(ctx, "system", sysB.ID)
	if err != nil {
		t.Fatalf("list roles on sysB by id: %v", err)
	}
	if len(rolesB) != 0 {
		t.Fatalf("sysB roles = %+v, want none (cross-contaminated by the shared name)", rolesB)
	}

	// Re-declaring (an upsert on owner_kind/owner_arc/name) must still land on
	// sysA only.
	if _, err := gw.SetSystemRole(ctx, "", "system", sysA.ID, storage.SystemRoleSpec{
		Name: "seat", DisplayName: "Seat (revised)", Quorum: 1,
	}); err != nil {
		t.Fatalf("revise role on sysA by id: %v", err)
	}
	rolesA, err = gw.ListSystemRoles(ctx, "system", sysA.ID)
	if err != nil {
		t.Fatalf("list roles on sysA by id (after revise): %v", err)
	}
	if len(rolesA) != 1 || rolesA[0].DisplayName != "Seat (revised)" {
		t.Fatalf("sysA roles after revise = %+v, want the revised display name", rolesA)
	}

	if err := gw.DeleteSystemRole(ctx, "", "system", sysA.ID, "seat"); err != nil {
		t.Fatalf("delete role on sysA by id: %v", err)
	}
	rolesA, err = gw.ListSystemRoles(ctx, "system", sysA.ID)
	if err != nil {
		t.Fatalf("list roles on sysA by id (after delete): %v", err)
	}
	if len(rolesA) != 0 {
		t.Fatalf("sysA roles after delete = %+v, want none", rolesA)
	}

	// A genuinely unknown system owner still falls through to the arc CHECK
	// constraint's ErrRoleRefNotFound, unchanged (roleOwnerArg's documented
	// not-found behavior).
	_, err = gw.SetSystemRole(ctx, "", "system", "00000000-0000-0000-0000-000000000000", storage.SystemRoleSpec{
		Name: "seat", DisplayName: "Seat", Quorum: 1,
	})
	if !errors.Is(err, storage.ErrRoleRefNotFound) {
		t.Fatalf("declare role on an unknown system = %v, want ErrRoleRefNotFound", err)
	}
}

// TestDuplicateNamesDoNotBreakGatewayReads is the regression #627 Task 10 exists
// to prevent: once two rows legitimately share a name, every gateway path that
// resolves a reference through an inline `(select id from X where name = $1)`
// scalar subquery raises SQLSTATE 21000 ("more than one row returned by a
// subquery used as an expression") the moment that name is ambiguous, a hard 500
// out of alarm reads, member writes, role assignment, health recompute, and
// interface create. Addressing each duplicate by its uuid (never by the shared
// bare name) must work cleanly and land on the right row.
func TestDuplicateNamesDoNotBreakGatewayReads(t *testing.T) {
	gw, conn := newDuplicateNameFixture(t)
	ctx := context.Background()
	all := scope.Set{All: true}
	compA, compB, sys := twoDisplayOnes(t, gw)

	// Alarm create, by uuid: must not 21000, and must land on the right row.
	alarm, err := gw.RaiseAlarm(ctx, "", compA.ID, storage.AlarmSpec{Severity: "critical", Message: "m", DedupKey: "k"})
	if err != nil {
		t.Fatalf("raise alarm on compA by id: %v", err)
	}
	if alarm.ComponentID != compA.ID {
		t.Fatalf("alarm landed on %s, want %s", alarm.ComponentID, compA.ID)
	}
	aAlarms, err := gw.ListAlarms(ctx, compA.ID, storage.AlarmFilter{IncludeCleared: true})
	if err != nil {
		t.Fatalf("list alarms compA by id: %v", err)
	}
	if len(aAlarms) != 1 {
		t.Fatalf("compA alarms = %d, want 1", len(aAlarms))
	}
	bAlarms, err := gw.ListAlarms(ctx, compB.ID, storage.AlarmFilter{IncludeCleared: true})
	if err != nil {
		t.Fatalf("list alarms compB by id: %v", err)
	}
	if len(bAlarms) != 0 {
		t.Fatalf("compB alarms = %d, want 0 (cross-contaminated by the shared name)", len(bAlarms))
	}

	// Member write, by uuid.
	if err := gw.AddMember(ctx, "", sys.Name, compA.ID, all); err != nil {
		t.Fatalf("add member by id: %v", err)
	}
	memsA, err := gw.ComponentMemberships(ctx, compA.ID, all)
	if err != nil {
		t.Fatalf("compA memberships: %v", err)
	}
	if len(memsA) != 1 {
		t.Fatalf("compA memberships = %d, want 1", len(memsA))
	}
	memsB, err := gw.ComponentMemberships(ctx, compB.ID, all)
	if err != nil {
		t.Fatalf("compB memberships: %v", err)
	}
	if len(memsB) != 0 {
		t.Fatalf("compB memberships = %d, want 0 (cross-contaminated by the shared name)", len(memsB))
	}

	// Role assignment, by uuid.
	if err := gw.AssignRole(ctx, "", sys.Name, "seat", compA.ID, all); err != nil {
		t.Fatalf("assign role by id: %v", err)
	}
	var assignedComponent string
	if err := conn.QueryRow(ctx,
		`select component_id from system_role_assignment where system_id = $1`, sys.ID,
	).Scan(&assignedComponent); err != nil {
		t.Fatalf("read assignment: %v", err)
	}
	if assignedComponent != compA.ID {
		t.Fatalf("assignment landed on %s, want %s", assignedComponent, compA.ID)
	}

	// Health recompute: already exercised in-transaction by RaiseAlarm and
	// AssignRole above (both call it); read the result back explicitly too.
	rep, err := gw.SystemHealth(ctx, sys.Name, 0, all)
	if err != nil {
		t.Fatalf("system health: %v", err)
	}
	if rep.Verdict == "" {
		t.Fatalf("system health verdict came back empty")
	}
	if len(rep.Roles) != 1 || len(rep.Roles[0].AssignedTo) != 1 {
		t.Fatalf("system health roles = %+v, want one role with one assignee", rep.Roles)
	}
	crep, err := gw.RaiseAlarm(ctx, "", compB.ID, storage.AlarmSpec{Severity: "critical", Message: "m2", DedupKey: "k2"})
	if err != nil {
		t.Fatalf("raise alarm on compB by id: %v", err)
	}
	_ = crep

	// Interface create, by uuid.
	it, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Type: "tcp", Component: strptr(compA.ID)}, all)
	if err != nil {
		t.Fatalf("create interface by id: %v", err)
	}
	if it.ComponentID == nil || *it.ComponentID != compA.ID {
		t.Fatalf("interface component = %v, want %s", it.ComponentID, compA.ID)
	}
	ifs, err := gw.ListComponentInterfaces(ctx, compA.ID)
	if err != nil {
		t.Fatalf("list interfaces by id: %v", err)
	}
	if len(ifs) != 1 {
		t.Fatalf("compA interfaces = %d, want 1", len(ifs))
	}
	ifsB, err := gw.ListComponentInterfaces(ctx, compB.ID)
	if err != nil {
		t.Fatalf("list interfaces compB by id: %v", err)
	}
	if len(ifsB) != 0 {
		t.Fatalf("compB interfaces = %d, want 0 (cross-contaminated by the shared name)", len(ifsB))
	}
}

// TestBareNameAmbiguousRefuses proves scopedByName refuses an ambiguous bare
// name rather than silently picking one row and hiding the other: the read-side
// half of the same fix.
func TestBareNameAmbiguousRefuses(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	ctx := context.Background()
	all := scope.Set{All: true}
	compA, compB, _ := twoDisplayOnes(t, gw)

	_, err := gw.GetComponent(ctx, "display-1", all)
	var ambig *storage.ErrAmbiguousName
	if !errors.As(err, &ambig) {
		t.Fatalf("get by ambiguous bare name = %v, want *storage.ErrAmbiguousName", err)
	}
	if ambig.Kind != "component" {
		t.Errorf("kind = %q, want %q", ambig.Kind, "component")
	}
	if ambig.Ref != "display-1" {
		t.Errorf("ref = %q, want %q", ambig.Ref, "display-1")
	}
	if len(ambig.Candidates) != 2 {
		t.Fatalf("candidates = %v, want 2", ambig.Candidates)
	}
	got := map[string]bool{ambig.Candidates[0]: true, ambig.Candidates[1]: true}
	if !got[compA.ID] || !got[compB.ID] {
		t.Errorf("candidates = %v, want %s and %s", ambig.Candidates, compA.ID, compB.ID)
	}
}

// TestResolveTagsSurvivesDuplicateSystemNames is the regression for the #627
// review's finding 4: resolveTagsSQL's seed_sys CTE matched a caller's
// ?system= by bare name. Because seed_sys is a CTE and not a scalar
// subquery, an ambiguous name never raised SQLSTATE 21000, it silently
// seeded the cascade from every same-named system and unioned all of their
// tag bindings into one effective-tags answer, the same silent-wrong-answer
// shape TestDuplicateNamesDoNotBreakIngestPath's EffectiveProperties case
// covers. Addressing the wanted system by uuid must give a deterministic
// single-system answer; addressing it by the now-ambiguous bare name must
// refuse cleanly rather than silently pick or union.
func TestResolveTagsSurvivesDuplicateSystemNames(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	ctx := context.Background()
	all := scope.Set{All: true}
	sysA, sysB := twoSameNamedSystems(t, gw)

	// A device that is a member of BOTH same-named systems: the shared-device
	// case resolveTagsSQL's own comment documents as the reason ?system=
	// exists at all.
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "shared-dsp"}, all, all, all, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	if err := gw.AddMember(ctx, "", sysA.ID, comp.ID, all); err != nil {
		t.Fatalf("add member sysA by id: %v", err)
	}
	if err := gw.AddMember(ctx, "", sysB.ID, comp.ID, all); err != nil {
		t.Fatalf("add member sysB by id: %v", err)
	}

	// Propagates: true, since a system-band binding only reaches the component's
	// effective-tags read (band 2) when the key cascades; a non-propagating key
	// is admitted only from a binding on the component itself (band 3, depth 0).
	if _, err := gw.CreateTag(ctx, "", storage.TagSpec{Name: "role", Propagates: true}, all); err != nil {
		t.Fatalf("create tag: %v", err)
	}
	if _, err := gw.SetTagBinding(ctx, "", "role", "system", strptr(sysA.ID), "primary-video", all, all); err != nil {
		t.Fatalf("bind role on sysA by id: %v", err)
	}
	if _, err := gw.SetTagBinding(ctx, "", "role", "system", strptr(sysB.ID), "backup-video", all, all); err != nil {
		t.Fatalf("bind role on sysB by id: %v", err)
	}

	// Addressed by uuid: a deterministic single-system answer, never sysB's
	// value leaking in as a shadowed candidate.
	resolved, err := gw.ResolveTags(ctx, comp.ID, sysA.ID, all)
	if err != nil {
		t.Fatalf("resolve tags for sysA by id: %v", err)
	}
	var sawPrimary, sawBackup bool
	for _, r := range resolved {
		if r.Key != "role" {
			continue
		}
		switch r.Value {
		case "primary-video":
			sawPrimary = true
			if !r.Winner {
				t.Errorf("role=primary-video should be the winner, got %+v", r)
			}
		case "backup-video":
			sawBackup = true
		}
	}
	if !sawPrimary {
		t.Fatalf("resolved tags = %+v, want role=primary-video", resolved)
	}
	if sawBackup {
		t.Fatalf("resolved tags = %+v, leaked sysB's binding (cross-contaminated by the shared name)", resolved)
	}

	// Addressed by the now-ambiguous bare name: a clean refusal, not a silent
	// union and not a 500.
	_, err = gw.ResolveTags(ctx, comp.ID, "huddle-1", all)
	var ambig *storage.ErrAmbiguousName
	if !errors.As(err, &ambig) {
		t.Fatalf("resolve tags by ambiguous bare system name = %v, want *storage.ErrAmbiguousName", err)
	}
}

// TestStandardOwnedRoleDeclarationSurvivesDuplicateSystemNames is the
// regression for the #627 review's finding 3: conformingSystems selected only
// a conforming system's NAME, even though its id was right there in the same
// row (keyed on standard_id, an unambiguous FK), and recomputeSystems then
// re-resolved that name through scopedByName. A standard-owned role
// declaration re-derived an id from a name on every recompute, so the moment
// a conforming system shared its name with an unrelated system anywhere in
// the estate (the two need not even conform to the same standard),
// SetSystemRole and DeleteSystemRole failed with an unmapped
// *storage.ErrAmbiguousName on a declaration that was never actually
// ambiguous, and the whole write (including its audit row) rolled back.
//
// sysB deliberately never conforms to the standard, so it is the
// cross-contamination check: if conformingSystems' fix silently widened
// which systems the recompute reaches, sysB would show the role too.
//
// The cross-contamination check asserts on rep.Transitions, the RECORDED
// side (healthTransitions reads what recordHealth actually wrote, keyed on
// the id the recompute was given), not rep.Roles or rep.Verdict: both of
// those are computed LIVE from resolveHealthRoles against sysB's own id,
// entirely independent of which id conformingSystems fed the recompute, so a
// conformingSystems that returned the WRONG system's id would still leave
// sysB's live-computed Roles/Verdict looking correct (empty) while silently
// recording a health edge onto it. Only the recorded transitions would catch
// that.
func TestStandardOwnedRoleDeclarationSurvivesDuplicateSystemNames(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	ctx := context.Background()
	all := scope.Set{All: true}
	sysA, sysB := twoSameNamedSystems(t, gw)

	if err := gw.UpsertStandard(ctx, storage.Standard{Name: "dup-std", DisplayName: "Dup Standard"}); err != nil {
		t.Fatalf("create standard: %v", err)
	}
	if _, err := gw.UpdateSystem(ctx, "", sysA.ID, storage.SystemPatch{StandardID: strptr("dup-std")}, all, all); err != nil {
		t.Fatalf("conform sysA to standard by id: %v", err)
	}

	// sysB's recorded transitions before the standard-owned declaration: just
	// its own opening verdict from CreateSystem (recordHealth always records
	// the first value for an owner, even Healthy). Captured here so the
	// post-declaration read below can assert it is BYTE-FOR-BYTE unchanged,
	// not merely "still empty of the new role" (which live-computed Roles
	// already covers and cannot fail on this bug).
	repBBefore, err := gw.SystemHealth(ctx, sysB.ID, 0, all)
	if err != nil {
		t.Fatalf("system health sysB before declaration: %v", err)
	}
	if len(repBBefore.Transitions) == 0 {
		t.Fatalf("sysB transitions before declaration = %+v, want its own opening verdict recorded at creation", repBBefore.Transitions)
	}

	if _, err := gw.SetSystemRole(ctx, "", "standard", "dup-std", storage.SystemRoleSpec{
		Name: "seat", DisplayName: "Seat", Quorum: 1,
	}); err != nil {
		t.Fatalf("declare standard role: %v", err)
	}

	repA, err := gw.SystemHealth(ctx, sysA.ID, 0, all)
	if err != nil {
		t.Fatalf("system health sysA by id: %v", err)
	}
	if repA.Verdict == "" {
		t.Fatalf("sysA health verdict came back empty")
	}
	if len(repA.Roles) != 1 || repA.Roles[0].Name != "seat" {
		t.Fatalf("sysA roles = %+v, want one role named seat", repA.Roles)
	}
	// The RECORDED side: sysA must show a NEW transition to degraded (an
	// unstaffed quorum-1 role, default impact), proving recordHealth actually
	// wrote for sysA's own id. rep.Roles above says nothing about which id
	// the write landed on; this does.
	if n := len(repA.Transitions); n == 0 {
		t.Fatalf("sysA transitions = %+v, want at least one recorded edge", repA.Transitions)
	} else if last := repA.Transitions[n-1]; last.Value != "degraded" {
		t.Fatalf("sysA latest transition = %+v, want degraded (an unstaffed quorum-1 role)", last)
	}

	repB, err := gw.SystemHealth(ctx, sysB.ID, 0, all)
	if err != nil {
		t.Fatalf("system health sysB by id: %v", err)
	}
	if len(repB.Roles) != 0 {
		t.Fatalf("sysB roles = %+v, want none (cross-contaminated by the shared name)", repB.Roles)
	}
	if len(repB.Transitions) != len(repBBefore.Transitions) {
		t.Fatalf("sysB transitions = %+v, want unchanged from before the declaration %+v (cross-contaminated by the shared name: the recompute wrote to sysB's id)",
			repB.Transitions, repBBefore.Transitions)
	}

	if err := gw.DeleteSystemRole(ctx, "", "standard", "dup-std", "seat"); err != nil {
		t.Fatalf("withdraw standard role: %v", err)
	}
	repA2, err := gw.SystemHealth(ctx, sysA.ID, 0, all)
	if err != nil {
		t.Fatalf("system health sysA after withdraw: %v", err)
	}
	if len(repA2.Roles) != 0 {
		t.Fatalf("sysA roles after withdraw = %+v, want none", repA2.Roles)
	}
	if n := len(repA2.Transitions); n == 0 {
		t.Fatalf("sysA transitions after withdraw = %+v, want at least one recorded edge", repA2.Transitions)
	} else if last := repA2.Transitions[n-1]; last.Value != "healthy" {
		t.Fatalf("sysA latest transition after withdraw = %+v, want healthy (the recorded recovery)", last)
	}
	repB2, err := gw.SystemHealth(ctx, sysB.ID, 0, all)
	if err != nil {
		t.Fatalf("system health sysB after withdraw: %v", err)
	}
	if len(repB2.Transitions) != len(repBBefore.Transitions) {
		t.Fatalf("sysB transitions after withdraw = %+v, want still unchanged from before the declaration %+v",
			repB2.Transitions, repBBefore.Transitions)
	}
}
