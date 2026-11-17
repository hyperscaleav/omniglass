package cli

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// platformHealthRig is a seeded install with one server component carrying
// its boot identity and an ingest lag, served by a lab handler: what
// `omniglass platform health` reads on a real server.
type platformHealthRig struct {
	ctx    context.Context
	conn   *pgx.Conn
	url    string
	owner  string
	server string
}

func newPlatformHealthRig(t *testing.T) *platformHealthRig {
	t.Helper()
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("gw: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { conn.Close(ctx) })
	// The narrow role goes in before the handler loads its role index.
	if _, err := conn.Exec(ctx, `insert into role (name, official, permissions, inherits) values ('node-reader', false, '{node:read}', '{}')`); err != nil {
		t.Fatalf("insert role: %v", err)
	}
	server, err := gw.EnsureServerComponent(ctx, "lab-server")
	if err != nil {
		t.Fatalf("ensure server component: %v", err)
	}
	if err := gw.RecordServerIdentity(ctx, server, "v9.9.9", time.Now().UTC().Add(-time.Hour)); err != nil {
		t.Fatalf("record server identity: %v", err)
	}
	if err := gw.RecordPlatformMetrics(ctx, []storage.PlatformMetric{
		{ComponentID: server, Key: writerclass.IngestLag, Value: 2.5, TS: time.Now().UTC()},
	}); err != nil {
		t.Fatalf("record the server's figures: %v", err)
	}
	srv := httptest.NewServer(api.NewHandler(gw, api.WithLab(true)))
	t.Cleanup(srv.Close)
	return &platformHealthRig{ctx: ctx, conn: conn, url: srv.URL, owner: tok, server: server}
}

func (r *platformHealthRig) health(t *testing.T, tok string, args ...string) (string, error) {
	t.Helper()
	return runCLI(t, append(append([]string{"platform", "health"}, args...), "--server", r.url, "--token", tok)...)
}

// nodeReader mints a service principal granted only node:read at the all
// scope: it may open the platform view but read none of its figures.
func (r *platformHealthRig) nodeReader(t *testing.T) string {
	t.Helper()
	var pid string
	if err := r.conn.QueryRow(r.ctx, `insert into principal (kind) values ('service') returning id`).Scan(&pid); err != nil {
		t.Fatalf("insert principal: %v", err)
	}
	if _, err := r.conn.Exec(r.ctx, `insert into service (principal_id, name) values ($1, 'fleet-node-reader')`, pid); err != nil {
		t.Fatalf("insert service: %v", err)
	}
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	if _, err := r.conn.Exec(r.ctx, `insert into credential (principal_id, kind, secret_hash, prefix) values ($1, 'bearer', $2, $3)`, pid, hash, prefix); err != nil {
		t.Fatalf("insert credential: %v", err)
	}
	if _, err := r.conn.Exec(r.ctx, `insert into principal_grant (principal_id, role_id, scope_kind, scope_id, scope_op)
		values ($1, (select id from role where name = 'node-reader'), 'all', null, 'subtree')`, pid); err != nil {
		t.Fatalf("insert grant: %v", err)
	}
	return tok
}

// TestPlatformHealthReadsTheServerFromTheAPI: against a real server, the
// owner's reading names the server, its build, and its ingest lag, and a
// platform with nothing wrong exits zero, as the JSON's healthy field says.
// Skipped under -short.
func TestPlatformHealthReadsTheServerFromTheAPI(t *testing.T) {
	r := newPlatformHealthRig(t)

	got, err := r.health(t, r.owner)
	if err != nil {
		t.Fatalf("omniglass platform health: %v\n%s", err, got)
	}
	for _, want := range []string{"lab-server", "v9.9.9", "healthy"} {
		if !strings.Contains(got, want) {
			t.Errorf("table missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "not visible to you") {
		t.Errorf("the owner's table hides a part:\n%s", got)
	}

	raw, err := r.health(t, r.owner, "--json")
	if err != nil {
		t.Fatalf("omniglass platform health --json: %v\n%s", err, raw)
	}
	var reading platformReading
	if err := json.Unmarshal([]byte(raw), &reading); err != nil {
		t.Fatalf("--json printed %q, not a reading: %v", raw, err)
	}
	if !reading.Healthy || len(reading.Reasons) != 0 {
		t.Errorf("healthy = %v, reasons = %v; want a healthy platform", reading.Healthy, reading.Reasons)
	}
	if len(reading.Servers) != 1 || reading.Servers[0].Name != "lab-server" {
		t.Fatalf("servers = %+v, want lab-server", reading.Servers)
	}
	var lag *platformFigureValue
	for i, f := range reading.Servers[0].Figures {
		if f.Key == writerclass.IngestLag {
			lag = &reading.Servers[0].Figures[i]
		}
	}
	if lag == nil || lag.Value != 2.5 {
		t.Errorf("ingest lag = %+v, want 2.5 from the recorded figure", lag)
	}
}

// TestPlatformHealthExitsNonzeroOnAnOpenAlarm: an open alarm on the server
// makes the reading not healthy, names the alarm, and fails the command, so a
// script or a probe can branch on the exit alone.
func TestPlatformHealthExitsNonzeroOnAnOpenAlarm(t *testing.T) {
	r := newPlatformHealthRig(t)
	if _, err := r.conn.Exec(r.ctx, `insert into alarm (component_id, severity, message, dedup_key)
		values ($1, 'high', 'ingest lag over a minute', 'ingest-lag-high')`, r.server); err != nil {
		t.Fatalf("raise an alarm: %v", err)
	}

	got, err := r.health(t, r.owner)
	if err == nil {
		t.Fatalf("omniglass platform health exited zero with an open alarm:\n%s", got)
	}
	if !strings.Contains(err.Error(), "the platform is not healthy") || !strings.Contains(err.Error(), "ingest lag over a minute") {
		t.Errorf("error = %q, want it to name the open alarm", err)
	}
	if !strings.Contains(got, "ALARMS") || !strings.Contains(got, "not healthy") {
		t.Errorf("table does not show the alarm and the verdict:\n%s", got)
	}
}

// TestPlatformHealthSaysWhatTheCallerCannotSee: a token with node:read alone
// opens the view but reads no figures; the table says each hidden part is not
// visible rather than printing it empty, and the command fails, since a reading
// that cannot see the platform cannot vouch for it.
func TestPlatformHealthSaysWhatTheCallerCannotSee(t *testing.T) {
	r := newPlatformHealthRig(t)
	tok := r.nodeReader(t)

	got, err := r.health(t, tok)
	if err == nil {
		t.Fatalf("omniglass platform health exited zero for a reader who sees no figures:\n%s", got)
	}
	if !strings.Contains(got, "not visible to you") {
		t.Errorf("table does not say what is hidden:\n%s", got)
	}
	if strings.Contains(got, "v9.9.9") {
		t.Errorf("a node:read-only token sees the server's build:\n%s", got)
	}
	if !strings.Contains(err.Error(), "not visible to you") {
		t.Errorf("error = %q, want it to say the figures are not visible", err)
	}
}
