package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestIssueCommandParamsSchemaEnforced closes the third silent schema (#595):
// params_schema is stored, documented, and CLI-surfaced, and before this slice
// IssueCommand never validated params against it, so a violating payload was
// recorded as if it satisfied the contract. Issuing now validates before any
// write, reject-not-project: a violating payload refuses with the named
// sentinel and leaves no command row and no caused event behind.
func TestIssueCommandParamsSchemaEnforced(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	// The caused event needs the seeded command-issued event_type.
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	conn := connectDSN(t, dsn)
	all := scope.Set{All: true}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: make([]byte, 32), Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap owner: %v", err)
	}
	var actor string
	if err := conn.QueryRow(ctx, `select principal_id from human where username = 'root'`).Scan(&actor); err != nil {
		t.Fatalf("resolve actor: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "amp-1"}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if _, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{
		Name: "set-level",
		ParamsSchema: []byte(`{"type":"object","required":["level"],
			"properties":{"level":{"type":"integer","minimum":0,"maximum":100}}}`),
	}); err != nil {
		t.Fatalf("create command type: %v", err)
	}

	// commandCount reads how many set-level invocations were recorded, so a
	// refusal can prove it projected nothing.
	commandCount := func(t *testing.T) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, `select count(*) from command
			where command_type_id = (select id from command_type where name = 'set-level')`).Scan(&n); err != nil {
			t.Fatalf("count commands: %v", err)
		}
		return n
	}

	t.Run("violating params refuse with the sentinel and write nothing", func(t *testing.T) {
		_, err := gw.IssueCommand(ctx, actor, "component", "amp-1", "set-level", "",
			nil, json.RawMessage(`{"level":"loud"}`), all)
		if !errors.Is(err, storage.ErrCommandParamsInvalid) {
			t.Fatalf("issue with violating params = %v, want ErrCommandParamsInvalid", err)
		}
		if n := commandCount(t); n != 0 {
			t.Errorf("a refused command left %d command row(s), want 0 (reject, not project)", n)
		}
		var events int
		if err := conn.QueryRow(ctx, `select count(*) from event where message = 'command set-level issued'`).Scan(&events); err != nil {
			t.Fatalf("count caused events: %v", err)
		}
		if events != 0 {
			t.Errorf("a refused command left %d caused event(s), want 0", events)
		}
	})

	t.Run("absent params against a schema with required fields refuse", func(t *testing.T) {
		_, err := gw.IssueCommand(ctx, actor, "component", "amp-1", "set-level", "", nil, nil, all)
		if !errors.Is(err, storage.ErrCommandParamsInvalid) {
			t.Fatalf("issue with absent params = %v, want ErrCommandParamsInvalid", err)
		}
	})

	t.Run("params within the schema issue as before", func(t *testing.T) {
		cmd, err := gw.IssueCommand(ctx, actor, "component", "amp-1", "set-level", "",
			nil, json.RawMessage(`{"level":50}`), all)
		if err != nil {
			t.Fatalf("issue with conforming params: %v", err)
		}
		if cmd.ID == 0 {
			t.Errorf("issued command has no id")
		}
	})

	t.Run("a command type without a schema takes any params", func(t *testing.T) {
		if _, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{Name: "free-cmd"}); err != nil {
			t.Fatalf("create command type: %v", err)
		}
		if _, err := gw.IssueCommand(ctx, actor, "component", "amp-1", "free-cmd", "",
			nil, json.RawMessage(`{"anything":true}`), all); err != nil {
			t.Fatalf("issue without a schema: %v", err)
		}
	})
}
