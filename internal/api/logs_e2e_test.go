package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// logsResp mirrors the log read body: the component and its recent raw log lines.
type logsResp struct {
	Component string `json:"component"`
	Logs      []struct {
		TS         time.Time       `json:"ts"`
		Source     string          `json:"source"`
		Severity   string          `json:"severity"`
		Facility   string          `json:"facility"`
		Message    string          `json:"message"`
		Attributes json.RawMessage `json:"attributes"`
	} `json:"logs"`
}

// TestLogsAPI drives the per-component raw-log read over HTTP (ADR-0066): an owner
// sees a component's recent log lines newest-first with severity and attributes; a
// viewer out of scope on the component gets a non-disclosing 404 (permission gate +
// scope injection), and sees its own in-scope component's empty log. Skipped under
// -short.
func TestLogsAPI(t *testing.T) {
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}

	t0 := time.Now().UTC().Add(-2 * time.Minute)
	t1 := t0.Add(time.Minute)
	if err := gw.InsertLogLines(ctx, []storage.LogLineWrite{
		{OwnerKind: "component", OwnerID: "disp-1", Source: "syslog", Severity: "info", Facility: "kern", Message: "link up", TS: t0},
		{OwnerKind: "component", OwnerID: "disp-1", Source: "syslog", Severity: "err", Facility: "daemon", Message: "codec unreachable", Attributes: []byte(`{"code":504}`), TS: t1},
	}); err != nil {
		t.Fatalf("insert log lines: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	out := c.do(ownerTok, http.MethodGet, "/components/disp-1/logs", nil, http.StatusOK)
	var r logsResp
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("decode logs: %v", err)
	}
	if r.Component != "disp-1" || len(r.Logs) != 2 {
		t.Fatalf("logs: want disp-1 with 2 lines, got %+v", r)
	}
	// Newest first: the err/daemon line with its structured attributes.
	if r.Logs[0].Message != "codec unreachable" || r.Logs[0].Severity != "err" || r.Logs[0].Facility != "daemon" {
		t.Fatalf("newest log: want 'codec unreachable' err/daemon, got %+v", r.Logs[0])
	}
	if string(r.Logs[0].Attributes) != `{"code":504}` {
		t.Fatalf("newest log attributes: want {code 504}, got %s", r.Logs[0].Attributes)
	}
	if r.Logs[1].Message != "link up" || r.Logs[1].Severity != "info" {
		t.Fatalf("oldest log: want 'link up' info, got %+v", r.Logs[1])
	}

	// A viewer out of scope on disp-1 gets a non-disclosing 404; its own in-scope
	// component returns an empty log.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "other-1"}, all, all, all); err != nil {
		t.Fatalf("create other component: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var otherID string
	if err := conn.QueryRow(ctx, `select id from component where name = 'other-1'`).Scan(&otherID); err != nil {
		t.Fatalf("other id: %v", err)
	}
	viewerTok := setupScopedViewer(t, ctx, dsn, "viewer-other", "viewer", "component", otherID)
	c.do(viewerTok, http.MethodGet, "/components/disp-1/logs", nil, http.StatusNotFound)
	c.do(viewerTok, http.MethodGet, "/components/other-1/logs", nil, http.StatusOK)
}

// nodeLogsResp mirrors the node self-log read body: the node and its own lines.
type nodeLogsResp struct {
	Node string `json:"node"`
	Logs []struct {
		TS         time.Time       `json:"ts"`
		Source     string          `json:"source"`
		Severity   string          `json:"severity"`
		Facility   string          `json:"facility"`
		Message    string          `json:"message"`
		Attributes json.RawMessage `json:"attributes"`
	} `json:"logs"`
}

// TestNodeLogsAPI drives the per-node self-log read over HTTP (ADR-0066): a node's
// own operational log lines come back newest-first with severity and attributes,
// owner-bound to the node not a component. A node is estate-wide, so an all-scope
// viewer reads them (node:read), an unknown node is a 404, and an unauthenticated
// request is a 401. Skipped under -short.
func TestNodeLogsAPI(t *testing.T) {
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
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "site-a"}, all); err != nil {
		t.Fatalf("create node: %v", err)
	}

	t0 := time.Now().UTC().Add(-2 * time.Minute)
	t1 := t0.Add(time.Minute)
	if err := gw.InsertNodeLogs(ctx, []storage.NodeLogWrite{
		{Node: "site-a", Source: "node", Severity: "info", Facility: "enrollment", Message: "connected to bus", TS: t0},
		{Node: "site-a", Source: "collection", Severity: "warning", Facility: "collection", Message: "task skipped", Attributes: []byte(`{"task":"t-a"}`), TS: t1},
	}); err != nil {
		t.Fatalf("insert node logs: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	out := c.do(ownerTok, http.MethodGet, "/nodes/site-a/logs", nil, http.StatusOK)
	var r nodeLogsResp
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("decode node logs: %v", err)
	}
	if r.Node != "site-a" || len(r.Logs) != 2 {
		t.Fatalf("node logs: want site-a with 2 lines, got %+v", r)
	}
	// Newest first: the warning/collection line with its structured attributes.
	if r.Logs[0].Message != "task skipped" || r.Logs[0].Severity != "warning" || r.Logs[0].Source != "collection" {
		t.Fatalf("newest node log: want 'task skipped' warning/collection, got %+v", r.Logs[0])
	}
	if string(r.Logs[0].Attributes) != `{"task":"t-a"}` {
		t.Fatalf("newest node log attributes: want {task t-a}, got %s", r.Logs[0].Attributes)
	}
	if r.Logs[1].Message != "connected to bus" || r.Logs[1].Facility != "enrollment" {
		t.Fatalf("oldest node log: want 'connected to bus' enrollment, got %+v", r.Logs[1])
	}

	// A node is estate-wide: an all-scope viewer reads its logs (node:read). An
	// unknown node is a 404; an unauthenticated request is a 401.
	viewerTok := setupAllViewer(t, ctx, dsn, "viewer-node")
	c.do(viewerTok, http.MethodGet, "/nodes/site-a/logs", nil, http.StatusOK)
	c.do(ownerTok, http.MethodGet, "/nodes/ghost/logs", nil, http.StatusNotFound)
	c.do("", http.MethodGet, "/nodes/site-a/logs", nil, http.StatusUnauthorized)
}
