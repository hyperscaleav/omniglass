package storage_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// ADR-1442 (ruled 2026-10-29 on #1339, applied here by #1444): an audit row
// outlives its subject, so an update row carries each field it moved in both
// images and a delete row carries the whole subject in old. These tests read
// the rows straight off audit_log, after the subject is gone where the verb
// deletes it, because a row that only makes sense beside its subject is the
// defect.

// auditRow is one audit_log row's images, decoded.
type auditRow struct {
	Old, New map[string]any
	// Raw is the two images as stored, for the plaintext scan.
	Raw string
}

// latestAudit reads the newest audit row of one resource, id and verb.
func latestAudit(t *testing.T, dsn, resource, id, verb string) auditRow {
	t.Helper()
	return readAudit(t, dsn, `resource = $1 and resource_id = $2 and verb = $3`, resource, id, verb)
}

// latestAuditOf reads the newest audit row of one resource and verb, for a
// write whose subject id the caller never holds (a tombstone's).
func latestAuditOf(t *testing.T, dsn, resource, verb string) auditRow {
	t.Helper()
	return readAudit(t, dsn, `resource = $1 and verb = $2`, resource, verb)
}

func readAudit(t *testing.T, dsn, where string, args ...any) auditRow {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var oldRaw, newRaw []byte
	if err := conn.QueryRow(ctx, `select old, new from audit_log where `+where+` order by seq desc limit 1`,
		args...).Scan(&oldRaw, &newRaw); err != nil {
		t.Fatalf("read the audit row (%s %v): %v", where, args, err)
	}
	row := auditRow{Raw: string(oldRaw) + string(newRaw)}
	for raw, into := range map[*[]byte]*map[string]any{&oldRaw: &row.Old, &newRaw: &row.New} {
		if len(*raw) == 0 {
			continue
		}
		if err := json.Unmarshal(*raw, into); err != nil {
			t.Fatalf("decode image %s: %v", *raw, err)
		}
	}
	return row
}

// wantMoved fails unless field is in both images and differs between them.
func (r auditRow) wantMoved(t *testing.T, label, field string) {
	t.Helper()
	before, hasBefore := r.Old[field]
	after, hasAfter := r.New[field]
	if !hasBefore || !hasAfter {
		t.Errorf("%s: %q before %v (present %v), after %v (present %v); want it in both images (old %v, new %v)",
			label, field, before, hasBefore, after, hasAfter, r.Old, r.New)
		return
	}
	if b, _ := json.Marshal(before); string(b) == mustJSON(after) {
		t.Errorf("%s: %q is %v in both images, but the write moved it", label, field, before)
	}
}

// wantHeld fails unless the old image holds field at want (a delete row's subject).
func (r auditRow) wantHeld(t *testing.T, label, field string, want any) {
	t.Helper()
	got, ok := r.Old[field]
	if !ok || mustJSON(got) != mustJSON(want) {
		t.Errorf("%s: old %q = %v (present %v), want %v; old image %v", label, field, got, ok, want, r.Old)
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// wantNoPlaintext fails if any of the secrets' plaintext is anywhere in the row.
func (r auditRow) wantNoPlaintext(t *testing.T, label string, plaintexts ...string) {
	t.Helper()
	for _, p := range plaintexts {
		if strings.Contains(r.Raw, p) {
			t.Errorf("%s: the audit row carries the plaintext %q: %s", label, p, r.Raw)
		}
	}
}

func TestSettingsAuditRowsCarryTheirImages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, gw := newAuditGateway(t)

	if _, err := gw.UpsertSettingOverride(ctx, "", "platform", "ui", map[string]any{"theme": "omniglass-light"}, []string{"theme"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := gw.UpsertSettingOverride(ctx, "", "platform", "ui", map[string]any{"theme": "omniglass-dark"}, nil); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	row := latestAudit(t, dsn, "settings", "ui", "update")
	row.wantMoved(t, "settings update", "doc")
	row.wantMoved(t, "settings update", "locks")

	if _, err := gw.MergePatchSettingOverride(ctx, "", "platform", "ui", map[string]any{"theme": "omniglass-light"}); err != nil {
		t.Fatalf("merge patch: %v", err)
	}
	latestAudit(t, dsn, "settings", "ui", "update").wantMoved(t, "settings merge-patch", "doc")

	if err := gw.DeleteSettingOverride(ctx, "", "platform", "ui"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	del := latestAudit(t, dsn, "settings", "ui", "delete")
	del.wantHeld(t, "settings delete", "namespace", "ui")
	del.wantHeld(t, "settings delete", "doc", map[string]any{"theme": "omniglass-light"})

	// The factory reset deletes every row under one audit row: its old image is
	// all of them.
	for _, ns := range []string{"ui", "notifications"} {
		if _, err := gw.UpsertSettingOverride(ctx, "", "platform", ns, map[string]any{"k": ns}, nil); err != nil {
			t.Fatalf("upsert %s: %v", ns, err)
		}
	}
	if err := gw.DeleteAllSettingOverrides(ctx, "", "platform"); err != nil {
		t.Fatalf("delete all: %v", err)
	}
	all := latestAudit(t, dsn, "settings", "", "delete")
	if !strings.Contains(all.Raw, `"ui"`) || !strings.Contains(all.Raw, `"notifications"`) {
		t.Errorf("the factory reset's old image does not hold every deleted row: %s", all.Raw)
	}
}

func TestTagAuditRowsCarryTheirImages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, gw := newAuditGateway(t)
	all := scope.Set{All: true}

	if _, err := gw.CreateTag(ctx, "", storage.TagSpec{Name: "site", Label: "Site", AppliesTo: []string{"component"}}, all); err != nil {
		t.Fatalf("create: %v", err)
	}
	tg, err := gw.UpdateTag(ctx, "", "site", storage.TagPatch{
		AppliesTo: []string{"component", "system"}, AllowedValues: []string{"hq"},
	}, all)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	row := latestAudit(t, dsn, "tag", tg.ID, "update")
	row.wantMoved(t, "tag update", "applies_to")
	row.wantMoved(t, "tag update", "allowed_values")

	if err := gw.DeleteTag(ctx, "", "site", all); err != nil {
		t.Fatalf("delete: %v", err)
	}
	del := latestAudit(t, dsn, "tag", tg.ID, "delete")
	del.wantHeld(t, "tag delete", "name", "site")
	del.wantHeld(t, "tag delete", "label", "Site")
}

func TestVariableAuditRowsCarryTheirImages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, gw := newAuditGateway(t)
	all := scope.Set{All: true}

	v, err := gw.CreateVariable(ctx, "", storage.VariableSpec{
		Name: "poll-seconds", Label: "Poll", ValueType: "int", OwnerKind: "platform", Value: json.RawMessage(`30`),
	}, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := gw.UpdateVariable(ctx, "", v.ID, storage.VariablePatch{Value: json.RawMessage(`60`)}, all, all, true); err != nil {
		t.Fatalf("update: %v", err)
	}
	row := latestAudit(t, dsn, "variable", v.ID, "update")
	row.wantMoved(t, "variable update", "value")

	if err := gw.DeleteVariable(ctx, "", v.ID, all, all, true); err != nil {
		t.Fatalf("delete: %v", err)
	}
	del := latestAudit(t, dsn, "variable", v.ID, "delete")
	del.wantHeld(t, "variable delete", "name", "poll-seconds")
	del.wantHeld(t, "variable delete", "label", "Poll")
	del.wantHeld(t, "variable delete", "value", float64(60))
}

func TestSecretAuditRowsCarryTheirImagesButNeverThePlaintext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw, dsn := secretGateway(t)
	all := scope.Set{All: true}

	s, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "snmp", Label: "SNMP", SecretType: "snmp-community", OwnerKind: "platform",
		Fields: map[string]string{"community": "s3cr3t-ro"},
	}, all, true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := gw.UpdateSecret(ctx, "", s.ID, storage.SecretPatch{Fields: map[string]string{"community": "s3cr3t-rw"}}, all, all, true, true); err != nil {
		t.Fatalf("update value: %v", err)
	}
	row := latestAudit(t, dsn, "secret", s.ID, "update")
	row.wantMoved(t, "secret update", "version")
	row.wantNoPlaintext(t, "secret update", "s3cr3t-ro", "s3cr3t-rw")

	relabel := "SNMP v2"
	if _, err := gw.UpdateSecret(ctx, "", s.ID, storage.SecretPatch{Label: &relabel}, all, all, true, true); err != nil {
		t.Fatalf("update label: %v", err)
	}
	latestAudit(t, dsn, "secret", s.ID, "update").wantMoved(t, "secret relabel", "label")

	if err := gw.DeleteSecret(ctx, "", s.ID, all, all, true, true); err != nil {
		t.Fatalf("delete: %v", err)
	}
	del := latestAudit(t, dsn, "secret", s.ID, "delete")
	del.wantHeld(t, "secret delete", "name", "snmp")
	del.wantHeld(t, "secret delete", "secret_type", "snmp-community")
	del.wantNoPlaintext(t, "secret delete", "s3cr3t-ro", "s3cr3t-rw")
}

// newAuditGateway opens a seeded gateway on an ephemeral Postgres and returns its DSN beside it.
func newAuditGateway(t *testing.T) (string, *storage.PG) {
	t.Helper()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return dsn, gw
}

func TestWebhookTokenAuditRowsCarryTheirImages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw, dsn := secretGateway(t)
	all := scope.Set{All: true}

	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "hook-host"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	comp := "hook-host"
	ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "webhook", Component: &comp}, all)
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var secretID string
	if err := conn.QueryRow(ctx, `select secret_id from webhook_token where endpoint_id = $1`, ep.ID).Scan(&secretID); err != nil {
		t.Fatalf("find the token secret: %v", err)
	}
	var token string
	if err := conn.QueryRow(ctx, `select value::text from secret where id = $1`, secretID).Scan(&token); err != nil {
		t.Fatalf("read the sealed token: %v", err)
	}

	if _, err := gw.RotateWebhookToken(ctx, "", ep.ID, all, all); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	row := latestAudit(t, dsn, "secret", secretID, "update")
	row.wantMoved(t, "token rotation", "version")
	row.wantHeld(t, "token rotation", "name", "webhook-token")
	row.wantNoPlaintext(t, "token rotation", token)

	if err := gw.DeleteEndpoint(ctx, "", ep.ID, all, all); err != nil {
		t.Fatalf("delete endpoint: %v", err)
	}
	del := latestAudit(t, dsn, "secret", secretID, "delete")
	del.wantHeld(t, "token delete", "name", "webhook-token")
	del.wantHeld(t, "token delete", "secret_type", "webhook-token")
	del.wantNoPlaintext(t, "token delete", token)
}

func TestPropertyValueAuditRowsCarryTheirImages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, gw := newAuditGateway(t)
	all := scope.Set{All: true}

	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "prop-sys"}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	if _, err := gw.SetProperty(ctx, "", "system", "prop-sys", "serial-number", "", json.RawMessage(`"S-1"`), all, all); err != nil {
		t.Fatalf("set: %v", err)
	}
	pv, err := gw.SetProperty(ctx, "", "system", "prop-sys", "serial-number", "", json.RawMessage(`"S-2"`), all, all)
	if err != nil {
		t.Fatalf("re-set: %v", err)
	}
	row := latestAudit(t, dsn, "property", pv.ID, "update")
	row.wantMoved(t, "property update", "value")
	row.wantHeld(t, "property update", "value", "S-1")
	row.wantHeld(t, "property update", "property", "serial-number")

	if err := gw.ClearProperty(ctx, "", "system", "prop-sys", "serial-number", "", all, all); err != nil {
		t.Fatalf("clear: %v", err)
	}
	del := latestAuditOf(t, dsn, "property", "delete")
	del.wantHeld(t, "property delete", "value", "S-2")
	del.wantHeld(t, "property delete", "property", "serial-number")
	del.wantHeld(t, "property delete", "owner_id", "prop-sys")
}

// auditRoomFixture is two systems and a component, for the membership and
// role-assignment tests.
func auditRoomFixture(t *testing.T) (string, *storage.PG, scope.Set) {
	t.Helper()
	ctx := context.Background()
	dsn, gw := newAuditGateway(t)
	all := scope.Set{All: true}
	for _, s := range []string{"room-a", "room-b"} {
		if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: s}, all, all); err != nil {
			t.Fatalf("system %s: %v", s, err)
		}
	}
	product := storagetest.MintProduct(t, ctx, gw, "").Name
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "dsp", ProductName: &product}, all, all, all, all); err != nil {
		t.Fatalf("component: %v", err)
	}
	return dsn, gw, all
}

func TestMemberAuditRowsCarryTheirImages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, gw, all := auditRoomFixture(t)

	if err := gw.AddMember(ctx, "", "room-a", "dsp", all, all, all, all); err != nil {
		t.Fatalf("add a: %v", err)
	}
	if err := gw.AddMember(ctx, "", "room-b", "dsp", all, all, all, all); err != nil {
		t.Fatalf("add b: %v", err)
	}
	b := sysID(t, dsn, "room-b")
	add := latestAudit(t, dsn, "system_member", b, "update")
	add.wantMoved(t, "member add", "member")
	add.wantHeld(t, "member add", "member", false)
	add.wantHeld(t, "member add", "is_primary", false)

	if err := gw.SetPrimaryMember(ctx, "", "room-b", "dsp", all, all, all, all); err != nil {
		t.Fatalf("set primary: %v", err)
	}
	prim := latestAudit(t, dsn, "system_member", b, "update")
	prim.wantMoved(t, "member primary", "is_primary")
	prim.wantHeld(t, "member primary", "component", "dsp")

	if err := gw.RemoveMember(ctx, "", "room-b", "dsp", all, all, all, all); err != nil {
		t.Fatalf("remove: %v", err)
	}
	del := latestAudit(t, dsn, "system_member", b, "delete")
	del.wantHeld(t, "member remove", "system", "room-b")
	del.wantHeld(t, "member remove", "component", "dsp")
	del.wantHeld(t, "member remove", "member", true)
	del.wantHeld(t, "member remove", "is_primary", true)
}

// sysID reads a system's id by name.
func sysID(t *testing.T, dsn, name string) string {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var id string
	if err := conn.QueryRow(ctx, `select id::text from system where name = $1`, name).Scan(&id); err != nil {
		t.Fatalf("system %s id: %v", name, err)
	}
	return id
}

func TestRoleAssignmentAuditRowsCarryTheirImages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, gw, all := auditRoomFixture(t)
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "amp"}, all, all, all, all); err != nil {
		t.Fatalf("component amp: %v", err)
	}
	if _, err := gw.SetSystemRole(ctx, "", "system", "room-a", storage.SystemRoleSpec{Name: "mic", Label: "Mic", Quorum: 1, Impact: "none"}); err != nil {
		t.Fatalf("role: %v", err)
	}
	for _, c := range []string{"dsp", "amp"} {
		if err := gw.AssignRole(ctx, "", "room-a", "mic", c, all, all, all, all); err != nil {
			t.Fatalf("assign %s: %v", c, err)
		}
	}
	assign := latestAuditOf(t, dsn, "system_role_assignment", "update")
	assign.wantMoved(t, "assign", "assigned")
	assign.wantHeld(t, "assign", "assigned", false)
	assign.wantHeld(t, "assign", "component", "amp")
	assign.wantHeld(t, "assign", "system", "room-a")

	if err := gw.SwapPositions(ctx, "", "room-a", "mic", 1, 2, all, all); err != nil {
		t.Fatalf("swap: %v", err)
	}
	swap := latestAuditOf(t, dsn, "system_role_assignment", "update")
	swap.wantMoved(t, "swap", "occupants")
	swap.wantHeld(t, "swap", "occupants", []map[string]any{
		{"position": 1, "component": "dsp"}, {"position": 2, "component": "amp"},
	})

	if err := gw.UnassignRole(ctx, "", "room-a", "mic", "dsp", all, all, all, all); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	del := latestAuditOf(t, dsn, "system_role_assignment", "delete")
	del.wantHeld(t, "unassign", "component", "dsp")
	del.wantHeld(t, "unassign", "role", "mic")
	del.wantHeld(t, "unassign", "assigned", true)
	del.wantHeld(t, "unassign", "position", 2)
}

func TestRoleChoiceAuditRowsCarryTheirSubjectOnDelete(t *testing.T) {
	t.Parallel()
	gw, ctx, dsn := newChoiceTestGateway(t)
	std := "audit-choice-std"
	if err := gw.UpsertStandard(ctx, storage.Standard{Name: std, Label: "Audit Choice"}); err != nil {
		t.Fatalf("standard: %v", err)
	}
	if _, err := gw.SeedRoleChoice(ctx, "standard", std, storage.RoleChoiceSpec{
		Name: "conferencing", Label: "Conferencing",
		Alternates: []storage.AlternateSpec{{Name: "all-in-one", Label: "All-in-one"}, {Name: "split", Label: "Split"}},
	}); err != nil {
		t.Fatalf("seed choice: %v", err)
	}

	if err := gw.DeleteAlternate(ctx, "", "standard", std, "conferencing", "split"); err != nil {
		t.Fatalf("delete alternate: %v", err)
	}
	alt := latestAuditOf(t, dsn, "choice_alternate", "delete")
	alt.wantHeld(t, "alternate delete", "name", "split")
	alt.wantHeld(t, "alternate delete", "label", "Split")
	alt.wantHeld(t, "alternate delete", "choice", "conferencing")
	alt.wantHeld(t, "alternate delete", "position", 2)

	if err := gw.DeleteChoice(ctx, "", "standard", std, "conferencing"); err != nil {
		t.Fatalf("delete choice: %v", err)
	}
	choice := latestAuditOf(t, dsn, "role_choice", "delete")
	choice.wantHeld(t, "choice delete", "name", "conferencing")
	choice.wantHeld(t, "choice delete", "label", "Conferencing")
	choice.wantHeld(t, "choice delete", "owner_kind", "standard")
	choice.wantHeld(t, "choice delete", "alternates", []map[string]any{{"name": "all-in-one", "label": "All-in-one", "position": 1}})
}

func TestPrincipalAuditRowsCarryTheirImages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, gw := newAuditGateway(t)
	if err := gw.UpsertRole(ctx, storage.Role{Name: "owner", Official: true, Permissions: []string{"*:*"}}); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: make([]byte, 32), Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	root, err := gw.AuthenticateBearer(ctx, make([]byte, 32))
	if err != nil {
		t.Fatalf("auth root: %v", err)
	}
	all := scope.Set{All: true}
	alice, err := gw.CreateHumanPrincipal(ctx, root.ID, storage.HumanSpec{Username: "alice", Email: "a@example.test", Label: "Alice"}, all)
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	newLabel := "Alice Two"
	if err := gw.UpdateHumanProfile(ctx, alice.ID, storage.HumanProfilePatch{Label: &newLabel}); err != nil {
		t.Fatalf("profile: %v", err)
	}
	prof := latestAudit(t, dsn, "principal", alice.ID, "update")
	prof.wantMoved(t, "profile", "label")
	prof.wantHeld(t, "profile", "label", "Alice")
	prof.wantHeld(t, "profile", "username", "alice")

	if err := gw.SetPrincipalAvatar(ctx, root.ID, alice.ID, "AAAAavatarbytes", all); err != nil {
		t.Fatalf("avatar: %v", err)
	}
	av := latestAudit(t, dsn, "principal", alice.ID, "set_avatar")
	av.wantMoved(t, "set avatar", "has_avatar")
	av.wantHeld(t, "set avatar", "has_avatar", false)
	av.wantNoPlaintext(t, "set avatar", "AAAAavatarbytes")
	if err := gw.ClearPrincipalAvatar(ctx, root.ID, alice.ID, all); err != nil {
		t.Fatalf("clear avatar: %v", err)
	}
	clr := latestAudit(t, dsn, "principal", alice.ID, "clear_avatar")
	clr.wantMoved(t, "clear avatar", "has_avatar")
	clr.wantHeld(t, "clear avatar", "has_avatar", true)

	const hash = "$argon2id$hashmarker"
	if err := gw.SetPrincipalPassword(ctx, root.ID, alice.ID, hash, all); err != nil {
		t.Fatalf("reset password: %v", err)
	}
	reset := latestAudit(t, dsn, "credential", alice.ID, "reset_password")
	reset.wantNoPlaintext(t, "reset password", hash, "hashmarker")

	if err := gw.ArchivePrincipal(ctx, root.ID, alice.ID, all); err != nil {
		t.Fatalf("archive: %v", err)
	}
	arch := latestAudit(t, dsn, "principal", alice.ID, "archive")
	arch.wantMoved(t, "archive", "archived")
	arch.wantHeld(t, "archive", "archived", false)
	arch.wantHeld(t, "archive", "active", true)

	if err := gw.PurgePrincipal(ctx, root.ID, alice.ID, all); err != nil {
		t.Fatalf("purge: %v", err)
	}
	purge := latestAudit(t, dsn, "principal", alice.ID, "purge")
	purge.wantHeld(t, "purge", "username", "alice")
	purge.wantHeld(t, "purge", "email", "a@example.test")
	purge.wantHeld(t, "purge", "label", "Alice Two")
	purge.wantHeld(t, "purge", "kind", "human")
	purge.wantHeld(t, "purge", "archived", true)
	purge.wantNoPlaintext(t, "purge", hash, "hashmarker")
}
