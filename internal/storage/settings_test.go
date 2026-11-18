package storage_test

import (
	"context"
	"errors"
	"testing"

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
