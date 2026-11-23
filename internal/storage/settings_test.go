package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestSettingBindingRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw := storagetest.NewDB(t) // ephemeral Postgres on a random port, migrations applied

	// upsert a platform override for ui
	if _, err := gw.UpsertSettingBinding(ctx, "", "platform", "ui",
		map[string]any{"theme": "omniglass-light"}, []string{"theme"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := gw.GetSettingBindings(ctx, "platform")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 1 || got[0].Namespace != "ui" || got[0].Doc["theme"] != "omniglass-light" {
		t.Fatalf("got %+v, want one ui row with theme=omniglass-light", got)
	}
	if len(got[0].Locks) != 1 || got[0].Locks[0] != "theme" {
		t.Fatalf("locks = %v, want [theme]", got[0].Locks)
	}

	// upsert again (update path), then delete to restore defaults
	if _, err := gw.UpsertSettingBinding(ctx, "", "platform", "ui",
		map[string]any{"theme": "omniglass-dark"}, nil); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	if err := gw.DeleteSettingBinding(ctx, "", "platform", "ui"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	after, err := gw.GetSettingBindings(ctx, "platform")
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("after delete want no rows, got %+v", after)
	}
}

// TestSettingBindingScopeIsConstrained pins the legal values of
// setting_binding.scope at the database. The column carried no constraint when the
// cascade's least-specific tier was renamed from 'global' to 'platform', so the Go
// layer went on writing the retired name, every operator override was orphaned in
// silence, and the whole suite stayed green. A CHECK makes that class of drift loud:
// only a cascade level that is actually persisted as a row may be written, so
// 'default' (reflected off the Settings struct) and 'file' (read from disk at boot)
// are rejected along with anything else.
func TestSettingBindingScopeIsConstrained(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw := storagetest.NewDB(t)

	if _, err := gw.UpsertSettingBinding(ctx, "", "platform", "ui",
		map[string]any{"theme": "omniglass-dark"}, nil); err != nil {
		t.Fatalf("upsert at the platform scope: %v", err)
	}

	for _, scope := range []string{"global", "default", "file", "platfrom", ""} {
		_, err := gw.UpsertSettingBinding(ctx, "", scope, "ui",
			map[string]any{"theme": "omniglass-dark"}, nil)
		if err == nil {
			t.Errorf("upsert at scope %q: want a check-constraint violation, got no error", scope)
			continue
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.ConstraintName != "setting_binding_scope_check" {
			t.Errorf("upsert at scope %q: err = %v, want a setting_binding_scope_check violation", scope, err)
		}
	}
}

func TestSettingBindingDeleteAll(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw := storagetest.NewDB(t)

	if _, err := gw.UpsertSettingBinding(ctx, "", "platform", "ui",
		map[string]any{"theme": "omniglass-light"}, nil); err != nil {
		t.Fatalf("upsert ui: %v", err)
	}
	if _, err := gw.UpsertSettingBinding(ctx, "", "platform", "keybindings",
		map[string]any{"open_edit": "x"}, nil); err != nil {
		t.Fatalf("upsert keybindings: %v", err)
	}

	if err := gw.DeleteAllSettingBindings(ctx, "", "platform"); err != nil {
		t.Fatalf("delete all: %v", err)
	}
	after, err := gw.GetSettingBindings(ctx, "platform")
	if err != nil {
		t.Fatalf("get after reset: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("after reset want no rows, got %+v", after)
	}
}

// TestUserSettingBindingIsTheirsAlone pins the user level of the cascade
// (#2450): a person's merge patch lands on their own row, another person's
// rows and the platform level stay as they were, a second patch merges onto
// the first, and the platform read never sees a user row.
func TestUserSettingBindingIsTheirsAlone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw := storagetest.NewDB(t)
	defer gw.Close()
	if err := gw.UpsertRole(ctx, storage.Role{Name: "owner", Official: true, Permissions: []string{"*:*"}}); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	zeros := make([]byte, 32)
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: zeros, Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	root, _ := gw.AuthenticateBearer(ctx, zeros)
	alice, err := gw.CreatePerson(ctx, root.ID, storage.PersonSpec{Username: "alice"}, scope.Set{All: true})
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	if _, err := gw.MergePatchUserSettingBinding(ctx, alice.ID, "learn", map[string]any{"done": []any{"device-to-samples"}}); err != nil {
		t.Fatalf("patch alice: %v", err)
	}
	if _, err := gw.MergePatchUserSettingBinding(ctx, alice.ID, "ui", map[string]any{"theme": "omniglass-light"}); err != nil {
		t.Fatalf("patch alice ui: %v", err)
	}
	if _, err := gw.MergePatchUserSettingBinding(ctx, alice.ID, "learn", map[string]any{"done": []any{"device-to-samples", "rules-and-alarms"}}); err != nil {
		t.Fatalf("re-patch alice: %v", err)
	}

	got, err := gw.GetUserSettingBindings(ctx, alice.ID)
	if err != nil {
		t.Fatalf("get alice: %v", err)
	}
	if len(got) != 2 || got[0].Namespace != "learn" || got[1].Namespace != "ui" {
		t.Fatalf("alice rows = %+v, want learn and ui", got)
	}
	done, _ := got[0].Doc["done"].([]any)
	if len(done) != 2 || got[1].Doc["theme"] != "omniglass-light" {
		t.Fatalf("alice rows = %+v, want two lessons done and the light theme", got)
	}

	rootRows, err := gw.GetUserSettingBindings(ctx, root.ID)
	if err != nil {
		t.Fatalf("get root: %v", err)
	}
	if len(rootRows) != 0 {
		t.Fatalf("root rows = %+v, want none: alice's patch is hers alone", rootRows)
	}
	platform, err := gw.GetSettingBindings(ctx, "platform")
	if err != nil {
		t.Fatalf("get platform: %v", err)
	}
	if len(platform) != 0 {
		t.Fatalf("platform rows = %+v, want none: a user row is not a platform binding", platform)
	}

	// A user-scoped row without its person is refused by the scope CHECK.
	if _, err := gw.UpsertSettingBinding(ctx, "", "user", "ui", map[string]any{"theme": "omniglass-dark"}, nil); err == nil {
		t.Fatal("a user row with no user_id was written; want a setting_binding_scope_check violation")
	}
}
