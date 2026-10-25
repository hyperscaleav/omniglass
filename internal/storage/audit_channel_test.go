package storage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// An audited write records the channel it arrived through (#1162): a write made
// under WithAuditChannel carries the channel and the client the caller declared,
// one made without it carries neither, and the read side returns both. The
// channel rides the context the way the real actor does, so only server code
// that sets it (the MCP dispatch) can stamp it. Skipped under -short.
func TestAuditRowCarriesItsChannelAndClient(t *testing.T) {
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
	if err := gw.UpsertRole(ctx, storage.Role{Name: "owner", Official: true, Permissions: []string{"*:*"}}); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	zeros := make([]byte, 32)
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: zeros, Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	root, _ := gw.AuthenticateBearer(ctx, zeros)
	alice, err := gw.CreateHumanPrincipal(ctx, root.ID, storage.HumanSpec{Username: "alice"}, scope.Set{All: true})
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	label := func(s string) *string { return &s }
	if err := gw.UpdateHumanProfile(ctx, alice.ID, storage.HumanProfilePatch{Label: label("Direct")}); err != nil {
		t.Fatalf("direct edit: %v", err)
	}
	mcpCtx := storage.WithAuditChannel(ctx, storage.AuditChannelMCP, "claude-code/2.1.0")
	if err := gw.UpdateHumanProfile(mcpCtx, alice.ID, storage.HumanProfilePatch{Label: label("Via MCP")}); err != nil {
		t.Fatalf("mcp edit: %v", err)
	}
	if err := gw.WriteAuthEvent(mcpCtx, alice.ID, "login"); err != nil {
		t.Fatalf("auth event: %v", err)
	}

	rows, err := gw.ListAuditLog(ctx, storage.AuditFilter{Resource: "principal", Verb: "update", Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var direct, viaMCP *storage.AuditEntry
	for i := range rows {
		if rows[i].ResourceID != alice.ID {
			continue
		}
		if strings.Contains(string(rows[i].New), "Via MCP") {
			viaMCP = &rows[i]
		} else if strings.Contains(string(rows[i].New), "Direct") {
			direct = &rows[i]
		}
	}
	if direct == nil || viaMCP == nil {
		t.Fatalf("want a direct and an MCP update row for alice, got %+v", rows)
	}
	if direct.Channel != "" || direct.Client != "" {
		t.Errorf("direct row channel=%q client=%q, want both empty", direct.Channel, direct.Client)
	}
	if viaMCP.Channel != "mcp" || viaMCP.Client != "claude-code/2.1.0" {
		t.Errorf("mcp row channel=%q client=%q, want mcp and claude-code/2.1.0", viaMCP.Channel, viaMCP.Client)
	}

	auth, err := gw.ListAuditLog(ctx, storage.AuditFilter{Resource: "auth", Verb: "login", Limit: 1})
	if err != nil || len(auth) != 1 {
		t.Fatalf("auth rows = %+v, %v", auth, err)
	}
	if auth[0].Channel != "mcp" || auth[0].Client != "claude-code/2.1.0" {
		t.Errorf("auth row channel=%q client=%q, want the MCP stamp", auth[0].Channel, auth[0].Client)
	}

	// The column holds only a channel the server knows: a direct insert of any
	// other value is refused by its check, so no write path can invent one.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `insert into audit_log (verb, resource, channel) values ('create', 'probe', 'console')`); err == nil {
		t.Error("an unknown channel was accepted, want the check to refuse it")
	}
}

// The client is what an MCP client declared about itself, so it is held to a
// printable, bounded string before it is written: control characters go and a
// long name is cut, so a hostile declaration cannot forge a line in a log or
// swell the row.
func TestAuditClientIsSanitized(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"claude-code/2.1.0", "claude-code/2.1.0"},
		{"  spaced  ", "spaced"},
		{"evil\nforged line\x1b[31m", "evilforged line[31m"},
		{strings.Repeat("x", 300), strings.Repeat("x", 200)},
		{"", ""},
	} {
		if got := storage.SanitizeAuditClient(c.in); got != c.want {
			t.Errorf("SanitizeAuditClient(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
