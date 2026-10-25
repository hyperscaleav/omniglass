package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestFileAnswersToItsFilename pins #1133 on files: a filename reaches the one
// file the caller can see by it; two visible files sharing a filename refuse
// with both uuids (the only unique spelling a file has); a sensitive file is
// invisible to a caller without the admin tier, refusing exactly as an absent
// one does; and the uuid keeps working.
func TestFileAnswersToItsFilename(t *testing.T) {
	ctx, gw, _ := newFileGW(t)
	mk := func(name string, sensitive bool) string {
		t.Helper()
		f, err := gw.CreateFile(ctx, "", storage.FileSpec{Name: name, ContentType: "text/plain", Data: []byte(name), Sensitive: sensitive}, true)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		return f.ID
	}
	solo := mk("codec-firmware-2.1.4.txt", false)
	dup1, dup2 := mk("notes.txt", false), mk("notes.txt", false)
	mk("keys.txt", true)

	for _, ref := range []string{"codec-firmware-2.1.4.txt", solo} {
		f, data, err := gw.DownloadFile(ctx, ref, false)
		if err != nil || f.ID != solo || string(data) != "codec-firmware-2.1.4.txt" {
			t.Fatalf("download %q = %+v (err %v)", ref, f, err)
		}
	}

	_, err := gw.GetFile(ctx, "notes.txt", false)
	var unresolved *storage.ErrUnresolvedRef
	if !errors.As(err, &unresolved) || !errors.Is(err, storage.ErrFileNotFound) {
		t.Fatalf("get notes.txt = %v, want an ErrUnresolvedRef that is ErrFileNotFound", err)
	}
	if strings.Join(unresolved.Candidates, ",") != dup1+","+dup2 {
		t.Errorf("candidates = %v, want both uuids", unresolved.Candidates)
	}

	refusal := func(ref string) string {
		t.Helper()
		_, err := gw.GetFile(ctx, ref, false)
		var u *storage.ErrUnresolvedRef
		if !errors.As(err, &u) || len(u.Candidates) != 0 {
			t.Fatalf("get %q = %v, want an ErrUnresolvedRef with no candidates", ref, err)
		}
		return strings.ReplaceAll(u.Error(), ref, "REF")
	}
	if refusal("keys.txt") != refusal("absent.txt") {
		t.Errorf("a sensitive file refuses differently from an absent one without the admin tier")
	}
	if _, err := gw.GetFile(ctx, "keys.txt", true); err != nil {
		t.Errorf("get keys.txt with the admin tier: %v", err)
	}

	if err := gw.DeleteFile(ctx, "", "codec-firmware-2.1.4.txt", false); err != nil {
		t.Fatalf("delete by filename: %v", err)
	}
	if _, err := gw.GetFile(ctx, solo, false); !errors.Is(err, storage.ErrFileNotFound) {
		t.Errorf("get the deleted file by uuid = %v, want ErrFileNotFound", err)
	}
}

// groupNameFixture seeds a gateway with one human, one service account, and a
// group.
func groupNameFixture(t *testing.T) (context.Context, storage.Gateway, string, *storage.Group, string, string) {
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
	// An owner, so the deferred owner invariant lets a grant be revoked.
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: make([]byte, 32), Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap owner: %v", err)
	}
	alice, err := gw.CreateHumanPrincipal(ctx, "", storage.HumanSpec{Username: "alice", PasswordHash: "x"}, all)
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	svc := mustService(t, ctx, dsn, "ci-bot")
	g, err := gw.CreateGroup(ctx, "", storage.GroupSpec{Name: "night-shift"}, all)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	return ctx, gw, dsn, g, alice.ID, svc
}

func mustService(t *testing.T, ctx context.Context, dsn, name string) string {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var id string
	if err := conn.QueryRow(ctx, `insert into principal (kind) values ('service') returning id`).Scan(&id); err != nil {
		t.Fatalf("insert principal: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into service (principal_id, name) values ($1, $2)`, id, name); err != nil {
		t.Fatalf("insert service: %v", err)
	}
	return id
}

// TestPrincipalGroupAnswersToItsName pins #1133 on principal groups: every
// group route resolves the group by name or uuid, its members by username or a
// service account's name, and returns the uuid. The all-scope requirement runs
// first, so a scoped caller is refused the same way for a name that exists and
// one that does not.
func TestPrincipalGroupAnswersToItsName(t *testing.T) {
	ctx, gw, _, g, aliceID, svcID := groupNameFixture(t)

	for _, ref := range []string{"night-shift", g.ID} {
		got, err := gw.GetGroup(ctx, ref, all)
		if err != nil || got.ID != g.ID {
			t.Fatalf("get group %q = %+v (err %v)", ref, got, err)
		}
	}
	if err := gw.AddGroupMember(ctx, "", "night-shift", "alice", all); err != nil {
		t.Fatalf("add alice by username: %v", err)
	}
	if err := gw.AddGroupMember(ctx, "", "night-shift", "ci-bot", all); err != nil {
		t.Fatalf("add ci-bot by service name: %v", err)
	}
	members, err := gw.ListGroupMembers(ctx, "night-shift", all)
	if err != nil || len(members) != 2 {
		t.Fatalf("members = %+v (err %v), want alice and ci-bot", members, err)
	}
	got := map[string]bool{}
	for _, m := range members {
		got[m.PrincipalID] = true
	}
	if !got[aliceID] || !got[svcID] {
		t.Errorf("members = %v, want alice %s and ci-bot %s by uuid", got, aliceID, svcID)
	}
	if err := gw.RemoveGroupMember(ctx, "", "night-shift", "alice", all); err != nil {
		t.Fatalf("remove alice by username: %v", err)
	}
	grant, err := gw.CreateGroupGrant(ctx, "", "night-shift", storage.GrantSpec{Role: "viewer", ScopeKind: "all"}, all)
	if err != nil {
		t.Fatalf("grant to the group by name: %v", err)
	}
	if grant.GroupID == nil || *grant.GroupID != g.ID {
		t.Errorf("grant group = %v, want the group's uuid %s", grant.GroupID, g.ID)
	}
	if err := gw.RevokeGroupGrant(ctx, "", "night-shift", grant.ID, all); err != nil {
		t.Fatalf("revoke from the group by name: %v", err)
	}
	label := "Night shift"
	if upd, err := gw.UpdateGroup(ctx, "", "night-shift", storage.GroupPatch{Label: &label}, all); err != nil || upd.ID != g.ID {
		t.Fatalf("update by name = %+v (err %v)", upd, err)
	}
	if ren, err := gw.RenameGroup(ctx, "", "night-shift", "late-shift", all); err != nil || ren.ID != g.ID {
		t.Fatalf("rename by name = %+v (err %v)", ren, err)
	}

	_, err = gw.GetGroup(ctx, "night-shift", all)
	var unresolved *storage.ErrUnresolvedRef
	if !errors.As(err, &unresolved) || !errors.Is(err, storage.ErrGroupNotFound) || unresolved.Ref != "night-shift" {
		t.Errorf("get the old name = %v, want an ErrUnresolvedRef naming it", err)
	}

	scoped := scope.Set{IDs: []string{g.ID}}
	for _, ref := range []string{"late-shift", "no-such-group"} {
		if _, err := gw.GetGroup(ctx, ref, scoped); !errors.Is(err, storage.ErrPrincipalForbidden) {
			t.Errorf("scoped get %q = %v, want ErrPrincipalForbidden whether or not it exists", ref, err)
		}
	}
	if err := gw.DeleteGroup(ctx, "", "late-shift", all); err != nil {
		t.Fatalf("delete by name: %v", err)
	}
}

// TestPrincipalRefAcceptsAServiceName: a principal reference is a uuid, a
// human's username, or a service account's name; a word that is both a username
// and a service name names no one principal and is refused.
func TestPrincipalRefAcceptsAServiceName(t *testing.T) {
	ctx, gw, dsn, _, aliceID, svcID := groupNameFixture(t)
	for ref, want := range map[string]string{"alice": aliceID, "ci-bot": svcID, svcID: svcID} {
		got, err := gw.ResolvePrincipalRef(ctx, ref)
		if err != nil || got != want {
			t.Fatalf("resolve %q = %q (err %v), want %s", ref, got, err, want)
		}
	}
	mustService(t, ctx, dsn, "alice")
	_, err := gw.ResolvePrincipalRef(ctx, "alice")
	var unresolved *storage.ErrUnresolvedRef
	if !errors.As(err, &unresolved) || !errors.Is(err, storage.ErrPrincipalNotFound) || len(unresolved.Candidates) != 2 {
		t.Fatalf("resolve a shared name = %v, want an ErrUnresolvedRef naming both", err)
	}
	if _, err := gw.ResolvePrincipalRef(ctx, "nobody"); !errors.Is(err, storage.ErrPrincipalNotFound) {
		t.Errorf("resolve an unknown name = %v, want ErrPrincipalNotFound", err)
	}
}

// TestGrantScopeAcceptsAName pins the grant's scope root: a location, system or
// component by name or dotted address as well as by uuid, stored as the uuid.
func TestGrantScopeAcceptsAName(t *testing.T) {
	ctx, gw, _, g, aliceID, _ := groupNameFixture(t)
	mustLoc(t, gw, "campus", "campus", nil)
	mustLoc(t, gw, "room", "room", strptr("campus"))
	roomID := mustLocID(t, gw, "room")
	for _, ref := range []string{"campus.room", "room", roomID} {
		gr, err := gw.CreateGrant(ctx, "", aliceID, storage.GrantSpec{Role: "viewer", ScopeKind: "location", ScopeID: ref, ScopeOp: "self"}, all)
		if err != nil {
			t.Fatalf("grant scoped to %q: %v", ref, err)
		}
		if gr.ScopeID == nil || *gr.ScopeID != roomID {
			t.Errorf("grant scoped to %q stored %v, want the room's uuid", ref, gr.ScopeID)
		}
		if err := gw.RevokeGrant(ctx, "", aliceID, gr.ID, all); err != nil {
			t.Fatalf("revoke: %v", err)
		}
	}
	gr, err := gw.CreateGroupGrant(ctx, "", g.ID, storage.GrantSpec{Role: "viewer", ScopeKind: "location", ScopeID: "campus.room"}, all)
	if err != nil || gr.ScopeID == nil || *gr.ScopeID != roomID {
		t.Fatalf("group grant scoped to campus.room = %+v (err %v)", gr, err)
	}
	for _, ref := range []string{"no-such-room", "campus.no-such-room"} {
		if _, err := gw.CreateGrant(ctx, "", aliceID, storage.GrantSpec{Role: "viewer", ScopeKind: "location", ScopeID: ref}, all); !errors.Is(err, storage.ErrBadScope) {
			t.Errorf("grant scoped to %q = %v, want ErrBadScope", ref, err)
		}
	}
}
