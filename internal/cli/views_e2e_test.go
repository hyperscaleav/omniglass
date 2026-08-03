package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/cli"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestViewRunCLI drives a view through the GENERATED cobra surface against a
// live server: `omniglass view run component-reachability` prints the uniform
// ViewResult with real row content (the same rows the API returns), and an
// undeclared --param surfaces the server's 400 naming the parameter. The CLI
// command under test is generated from the OpenAPI document by cligen, so this
// is the API-first loop closed end to end. Skipped under -short.
func TestViewRunCLI(t *testing.T) {
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
	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `insert into interface (name, type, component, params) values
		('disp-1-icmp', (select id from interface_type where name = 'icmp'), (select id from component where name = 'disp-1'), '{"target":"10.0.0.1"}'::jsonb)`); err != nil {
		t.Fatalf("insert interface: %v", err)
	}
	if err := gw.InsertStateSamples(ctx, []storage.StateSampleWrite{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "interface.reachable", Instance: "disp-1-icmp", Value: "down", Source: "icmp", TS: time.Now().UTC()},
	}); err != nil {
		t.Fatalf("insert verdict: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()

	// The generated command prints the ViewResult; assert row CONTENT through
	// the column index, not just the exit status.
	var out bytes.Buffer
	root := cli.Root("test")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"view", "run", "component-reachability", "--server", srv.URL, "--token", ownerTok})
	if err := root.ExecuteContext(ctx); err != nil {
		t.Fatalf("view run: %v\n%s", err, out.String())
	}
	var vr struct {
		Columns []struct {
			Name string `json:"name"`
		} `json:"columns"`
		Rows [][]any `json:"rows"`
	}
	if err := json.Unmarshal(out.Bytes(), &vr); err != nil {
		t.Fatalf("decode CLI output: %v\n%s", err, out.String())
	}
	col := map[string]int{}
	for i, c := range vr.Columns {
		col[c.Name] = i
	}
	if len(vr.Rows) != 1 {
		t.Fatalf("rows = %d, want 1: %s", len(vr.Rows), out.String())
	}
	if vr.Rows[0][col["component"]] != "disp-1" || vr.Rows[0][col["state"]] != "down" {
		t.Errorf("row = %v, want disp-1 down", vr.Rows[0])
	}

	// An undeclared --param round-trips as the server's 400 naming the param:
	// the command exits non-zero and the printed body says which one.
	var bad bytes.Buffer
	root = cli.Root("test")
	root.SetOut(&bad)
	root.SetErr(&bad)
	root.SetArgs([]string{"view", "run", "component-reachability", "--server", srv.URL, "--token", ownerTok, "--param", "bogus=1"})
	if err := root.ExecuteContext(ctx); err == nil {
		t.Fatalf("undeclared param: want a non-zero exit, got success\n%s", bad.String())
	}
	if !strings.Contains(bad.String(), "bogus") {
		t.Errorf("400 output does not name the offending param:\n%s", bad.String())
	}
}
