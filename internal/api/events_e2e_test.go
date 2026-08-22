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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}

	t0 := time.Now().UTC().Add(-2 * time.Minute)
	t1 := t0.Add(time.Minute)
	if err := gw.InsertEvents(ctx, []storage.EventWrite{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "call-started", Message: "call started", Source: "xapi", TS: t0},
		{OwnerKind: "component", OwnerID: "disp-1", Key: "call-started", Message: "call joined by 4 participants", Attributes: []byte(`{"peer":"room-204","protocol":"sip"}`), Source: "xapi", TS: t1},
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
	// Newest first: "call joined by 4 participants" with its structured attributes, observed provenance.
	if r.Events[0].Message != "call joined by 4 participants" || r.Events[0].Key != "call-started" ||
		r.Events[0].Provenance != "observed" || r.Events[0].Source != "xapi" {
		t.Fatalf("newest event: want 'call joined by 4 participants' call-started observed, got %+v", r.Events[0])
	}
	// The wire form is JSON-compacted (encoding/json compacts a RawMessage), unlike
	// the DB's jsonb normalization which inserts a space after the colon.
	if string(r.Events[0].Attributes) != `{"peer":"room-204","protocol":"sip"}` {
		t.Fatalf("newest event attributes: want {peer room-204}, got %s", r.Events[0].Attributes)
	}
	if r.Events[1].Message != "call started" {
		t.Fatalf("oldest event: want 'call started', got %q", r.Events[1].Message)
	}

	// A viewer scoped to a different component (out of scope on disp-1) gets a
	// non-disclosing 404: the permission passes (*:read) but scope injection hides
	// the row. Its own in-scope component returns an (empty) log.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "other-1"}, all, all, all, all); err != nil {
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

// The system-scoped reads on the wire (#793): one call for the room's whole
// story, rows labeled by owner; a caller without system scope gets the
// non-disclosing 404 both routes share.
func TestSystemEventsAndLogsOnTheWire(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	all := scope.Set{All: true}
	sys, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "wired-room"}, all, all)
	if err != nil {
		t.Fatalf("create system: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "wired-bar"}, all, all, all, all); err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := gw.AddMember(ctx, "", "wired-room", "wired-bar", all, all); err != nil {
		t.Fatalf("add member: %v", err)
	}
	now := time.Now().UTC()
	if err := gw.InsertEvents(ctx, []storage.EventWrite{
		{OwnerKind: "component", OwnerID: "wired-bar", Key: "call-started", Message: "member event", Source: "xapi", TS: now},
	}); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	if err := gw.InsertLogLines(ctx, []storage.LogLineWrite{
		{OwnerKind: "component", OwnerID: "wired-bar", Message: "link up", Severity: "info", TS: now},
	}); err != nil {
		t.Fatalf("insert log: %v", err)
	}

	var ev struct {
		Events []struct {
			Owner     string `json:"owner"`
			OwnerKind string `json:"owner_kind"`
			Message   string `json:"message"`
		} `json:"events"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/systems/"+sys.ID+"/events", nil, http.StatusOK), &ev); err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(ev.Events) != 1 || ev.Events[0].Owner != "wired-bar" || ev.Events[0].OwnerKind != "component" {
		t.Fatalf("system events = %+v, want the member's row labeled", ev.Events)
	}

	var lg struct {
		Logs []struct {
			Component string `json:"component"`
			Message   string `json:"message"`
		} `json:"logs"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/systems/"+sys.ID+"/logs", nil, http.StatusOK), &lg); err != nil {
		t.Fatalf("read logs: %v", err)
	}
	if len(lg.Logs) != 1 || lg.Logs[0].Component != "wired-bar" || lg.Logs[0].Message != "link up" {
		t.Fatalf("system logs = %+v, want the member's line named", lg.Logs)
	}

	// A principal holding no grant at all dies at the permission gate (403,
	// both routes share the stamp); the scoped-away non-disclosing 404 is the
	// GetSystem pattern every system read already proves.
	noneTok := principalWithGrants(t, ctx, dsn, "no-scope-viewer", nil)
	c.do(noneTok, http.MethodGet, "/systems/"+sys.ID+"/events", nil, http.StatusForbidden)
	c.do(noneTok, http.MethodGet, "/systems/"+sys.ID+"/logs", nil, http.StatusForbidden)
}
