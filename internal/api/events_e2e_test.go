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

// eventsResp mirrors the event read body: the component and its recent occurrences.
type eventsResp struct {
	Component string `json:"component"`
	Events    []struct {
		TS         time.Time       `json:"ts"`
		Key        string          `json:"key"`
		Instance   string          `json:"instance"`
		Message    string          `json:"message"`
		Attributes json.RawMessage `json:"attributes"`
		Provenance string          `json:"provenance"`
		Source     string          `json:"source"`
	} `json:"events"`
}

// TestEventsAPI drives the per-component event log read over HTTP: an owner sees a
// component's recent occurrences newest-first with message and attributes; a viewer
// with no scope on the component gets a non-disclosing 404 (permission gate + scope
// injection), and sees its own in-scope component's (empty) log. Skipped under
// -short.
func TestEventsAPI(t *testing.T) {
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

	t0 := time.Now().UTC().Add(-2 * time.Minute)
	t1 := t0.Add(time.Minute)
	if err := gw.InsertEvents(ctx, []storage.EventOccurrence{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "syslog.line", Instance: "disp-1-ssh", Message: "link down", Source: "ssh", TS: t0},
		{OwnerKind: "component", OwnerID: "disp-1", Key: "syslog.line", Instance: "disp-1-ssh", Message: "link up", Attributes: []byte(`{"iface":"eth0"}`), Source: "ssh", TS: t1},
	}); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	out := c.do(ownerTok, http.MethodGet, "/components/disp-1/events", nil, http.StatusOK)
	var r eventsResp
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if r.Component != "disp-1" || len(r.Events) != 2 {
		t.Fatalf("events: want disp-1 with 2 events, got %+v", r)
	}
	// Newest first: "link up" with its structured attributes, observed provenance.
	if r.Events[0].Message != "link up" || r.Events[0].Key != "syslog.line" ||
		r.Events[0].Provenance != "observed" || r.Events[0].Source != "ssh" {
		t.Fatalf("newest event: want 'link up' syslog.line observed, got %+v", r.Events[0])
	}
	// The wire form is JSON-compacted (encoding/json compacts a RawMessage), unlike
	// the DB's jsonb normalization which inserts a space after the colon.
	if string(r.Events[0].Attributes) != `{"iface":"eth0"}` {
		t.Fatalf("newest event attributes: want {iface eth0}, got %s", r.Events[0].Attributes)
	}
	if r.Events[1].Message != "link down" {
		t.Fatalf("oldest event: want 'link down', got %q", r.Events[1].Message)
	}

	// A viewer scoped to a different component (out of scope on disp-1) gets a
	// non-disclosing 404: the permission passes (*:read) but scope injection hides
	// the row. Its own in-scope component returns an (empty) log.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "other-1"}, all); err != nil {
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
	c.do(viewerTok, http.MethodGet, "/components/disp-1/events", nil, http.StatusNotFound)
	c.do(viewerTok, http.MethodGet, "/components/other-1/events", nil, http.StatusOK)
}
