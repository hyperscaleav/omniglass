package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// A command's status is readable from issue to settlement over the API
// (#1155): one command by its id under its component, a component's history
// filtered by status and time, both gated by command:read with the
// component resolved through the caller's read sets, and an issue that can
// wait for the outcome, bounded in time and in how many may wait at once.

// commandRead is the read body the tests look at.
type commandRead struct {
	ID          int64           `json:"id"`
	Component   string          `json:"component"`
	ComponentID string          `json:"component_id"`
	CommandType string          `json:"command_type"`
	Intended    json.RawMessage `json:"intended"`
	TargetKind  string          `json:"target_kind"`
	Target      string          `json:"target"`
	Status      string          `json:"status"`
	Phase       string          `json:"phase"`
	SettleMode  string          `json:"settle_mode"`
	SettledBy   string          `json:"settled_by"`
	Reason      string          `json:"reason"`
	Reply       *string         `json:"reply"`
	Observed    *struct {
		Value json.RawMessage `json:"value"`
		At    time.Time       `json:"at"`
	} `json:"observed"`
	IssuedAt   time.Time  `json:"issued_at"`
	SettledAt  *time.Time `json:"settled_at"`
	Deadline   time.Time  `json:"deadline"`
	Issuer     string     `json:"issuer"`
	IssuerID   string     `json:"issuer_id"`
	Settlement string     `json:"settlement"`
}

type commandStatusRig struct {
	ctx      context.Context
	dsn      string
	gw       *storage.PG
	ownerTok string
}

func newCommandStatusRig(t *testing.T) *commandStatusRig {
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
	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	all := scope.Set{All: true}
	for _, name := range []string{"disp-1", "other-1"} {
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name}, all, all, all, all); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	for _, ct := range []storage.CommandTypeSpec{
		{Name: "set-input-now", Label: "Set input (now)", TargetPropertyType: "video-input", SettleWindowSeconds: 0},
		{Name: "set-input-soon", Label: "Set input (soon)", TargetPropertyType: "video-input", SettleWindowSeconds: 2},
		{Name: "set-input-later", Label: "Set input (later)", TargetPropertyType: "video-input", SettleWindowSeconds: 60},
	} {
		if _, err := gw.CreateCommandType(ctx, "", ct); err != nil {
			t.Fatalf("create command type %s: %v", ct.Name, err)
		}
	}
	if err := gw.InsertPropertySamples(ctx, storage.PushWriter(), []storage.PropertySampleWrite{{
		OwnerKind: "component", OwnerID: "disp-1", Key: "video-input", Value: "hdmi2", TS: time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("seed observed: %v", err)
	}
	return &commandStatusRig{ctx: ctx, dsn: dsn, gw: gw, ownerTok: ownerTok}
}

// role inserts an operator-defined role carrying exactly perms.
func (r *commandStatusRig) role(t *testing.T, name string, perms ...string) {
	t.Helper()
	conn, err := pgx.Connect(r.ctx, r.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(r.ctx)
	if _, err := conn.Exec(r.ctx, `insert into role (name, official, permissions, inherits) values ($1, false, $2, '{}')`, name, perms); err != nil {
		t.Fatalf("insert role %s: %v", name, err)
	}
}

func (r *commandStatusRig) componentID(t *testing.T, name string) string {
	t.Helper()
	c, err := r.gw.GetComponent(r.ctx, name, scope.Set{All: true})
	if err != nil {
		t.Fatal(err)
	}
	return c.ID
}

func decodeCommand(t *testing.T, raw []byte) commandRead {
	t.Helper()
	var c commandRead
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("decode command %s: %v", raw, err)
	}
	return c
}

// One command reads back whole, by its id under its component (addressed by
// name or uuid), and the issue answers with the same body plus the verdict.
func TestCommandStatusReads(t *testing.T) {
	r := newCommandStatusRig(t)
	srv := httptest.NewServer(api.NewHandler(r.gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: r.ctx, base: srv.URL}

	settled := decodeCommand(t, c.do(r.ownerTok, http.MethodPost, "/components/disp-1/commands:issue",
		map[string]any{"command_type": "set-input-now", "value": "hdmi2"}, http.StatusOK))
	if settled.Status != "settled" || settled.Settlement != "settled" || settled.Phase != "settled" || settled.SettledBy != "observation" {
		t.Fatalf("issue = %+v, want settled by observation", settled)
	}
	if settled.Observed == nil || string(settled.Observed.Value) != `"hdmi2"` || settled.Reason == "" {
		t.Fatalf("issue evidence = %+v reason %q, want the observed hdmi2 and a reason", settled.Observed, settled.Reason)
	}
	if settled.Issuer != "root" || settled.IssuerID == "" || settled.Component != "disp-1" || settled.TargetKind != "property" || settled.Target != "video-input" {
		t.Fatalf("issue identity = %+v", settled)
	}

	got := decodeCommand(t, c.do(r.ownerTok, http.MethodGet, fmt.Sprintf("/components/disp-1/commands/%d", settled.ID), nil, http.StatusOK))
	if got.ID != settled.ID || got.Status != "settled" || got.Reason != settled.Reason || got.SettledAt == nil || string(got.Intended) != `"hdmi2"` {
		t.Fatalf("read = %+v, want the issued command whole", got)
	}
	if got.Settlement != "" {
		t.Fatalf("a read carries a settlement verdict %q; only the issue computes one", got.Settlement)
	}
	byID := decodeCommand(t, c.do(r.ownerTok, http.MethodGet, fmt.Sprintf("/components/%s/commands/%d", r.componentID(t, "disp-1"), settled.ID), nil, http.StatusOK))
	if byID.ID != settled.ID {
		t.Fatalf("read by the component's uuid = %+v", byID)
	}

	failed := decodeCommand(t, c.do(r.ownerTok, http.MethodPost, "/components/disp-1/commands:issue",
		map[string]any{"command_type": "set-input-now", "value": "hdmi3"}, http.StatusOK))
	if failed.Status != "failed" || !strings.Contains(failed.Reason, "hdmi2") || !strings.Contains(failed.Reason, "hdmi3") {
		t.Fatalf("mismatch = status %q reason %q, want failed naming both values", failed.Status, failed.Reason)
	}

	// A command under another component, or none at all, is a 404.
	c.do(r.ownerTok, http.MethodGet, fmt.Sprintf("/components/other-1/commands/%d", settled.ID), nil, http.StatusNotFound)
	c.do(r.ownerTok, http.MethodGet, "/components/disp-1/commands/999999", nil, http.StatusNotFound)
	c.do(r.ownerTok, http.MethodGet, fmt.Sprintf("/components/no-such/commands/%d", settled.ID), nil, http.StatusNotFound)
}

// The history is newest first, narrowed by status and time, a page at a
// time; a malformed time is a 422 naming it.
func TestCommandStatusList(t *testing.T) {
	r := newCommandStatusRig(t)
	srv := httptest.NewServer(api.NewHandler(r.gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: r.ctx, base: srv.URL}

	var ids []int64
	for _, v := range []string{"hdmi2", "hdmi3", "hdmi2"} {
		ids = append(ids, decodeCommand(t, c.do(r.ownerTok, http.MethodPost, "/components/disp-1/commands:issue",
			map[string]any{"command_type": "set-input-now", "value": v}, http.StatusOK)).ID)
	}
	c.do(r.ownerTok, http.MethodPost, "/components/other-1/commands:issue", map[string]any{"command_type": "reboot"}, http.StatusOK)

	list := func(query string) (string, []commandRead) {
		t.Helper()
		var body struct {
			Component string        `json:"component"`
			Commands  []commandRead `json:"commands"`
		}
		if err := json.Unmarshal(c.do(r.ownerTok, http.MethodGet, "/components/disp-1/commands"+query, nil, http.StatusOK), &body); err != nil {
			t.Fatal(err)
		}
		return body.Component, body.Commands
	}
	comp, all := list("")
	if comp != "disp-1" || len(all) != 3 || all[0].ID != ids[2] || all[2].ID != ids[0] {
		t.Fatalf("history of %s = %+v, want three newest first", comp, all)
	}
	if _, failed := list("?status=failed"); len(failed) != 1 || failed[0].ID != ids[1] {
		t.Fatalf("failed = %+v, want the hdmi3 command", failed)
	}
	if _, page := list("?limit=1"); len(page) != 1 || page[0].ID != ids[2] {
		t.Fatalf("limit 1 = %+v, want the newest", page)
	}
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if _, none := list("?since=" + future); len(none) != 0 {
		t.Fatalf("since the future = %+v, want none", none)
	}
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if _, some := list("?since=" + past + "&until=" + future); len(some) != 3 {
		t.Fatalf("within the last hour = %d, want 3", len(some))
	}
	c.do(r.ownerTok, http.MethodGet, "/components/disp-1/commands?since=yesterday", nil, http.StatusUnprocessableEntity)
	c.do(r.ownerTok, http.MethodGet, "/components/disp-1/commands?status=pending", nil, http.StatusUnprocessableEntity)
}

// Reading a command needs command:read, and the component it hangs off is
// resolved through the caller's read sets: outside the component read it is
// the non-disclosing 404, inside it but outside command:read the truthful
// 403, and a principal with no read at all is refused at the gate.
func TestCommandStatusScope(t *testing.T) {
	r := newCommandStatusRig(t)
	r.role(t, "component-reader", "component:read")
	disp, other := r.componentID(t, "disp-1"), r.componentID(t, "other-1")
	srv := httptest.NewServer(api.NewHandler(r.gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: r.ctx, base: srv.URL}
	id := decodeCommand(t, c.do(r.ownerTok, http.MethodPost, "/components/disp-1/commands:issue",
		map[string]any{"command_type": "set-input-now", "value": "hdmi2"}, http.StatusOK)).ID
	one, list := fmt.Sprintf("/components/disp-1/commands/%d", id), "/components/disp-1/commands"

	inScope := setupScopedViewer(t, r.ctx, r.dsn, "viewer-disp", "viewer", "component", disp)
	c.do(inScope, http.MethodGet, one, nil, http.StatusOK)
	c.do(inScope, http.MethodGet, list, nil, http.StatusOK)

	outOfScope := setupScopedViewer(t, r.ctx, r.dsn, "viewer-other", "viewer", "component", other)
	for _, path := range []string{one, list} {
		status, body := c.send(outOfScope, http.MethodGet, path, nil)
		if status != http.StatusNotFound || strings.Contains(string(body), "hdmi2") {
			t.Fatalf("out of scope %s = %d %s, want a non-disclosing 404", path, status, body)
		}
	}

	componentOnly := setupScopedPrincipal(t, r.ctx, r.dsn, "component-only",
		grantSpec{role: "component-reader", scopeKind: "component", scopeID: disp},
		grantSpec{role: "viewer", scopeKind: "component", scopeID: other})
	c.do(componentOnly, http.MethodGet, one, nil, http.StatusForbidden)
	c.do(componentOnly, http.MethodGet, list, nil, http.StatusForbidden)

	none := principalWithGrants(t, r.ctx, r.dsn, "nothing", nil)
	c.do(none, http.MethodGet, one, nil, http.StatusForbidden)
	c.do(none, http.MethodGet, list, nil, http.StatusForbidden)
}

// An issue with wait blocks until the command is terminal, judging it at
// its deadline rather than waiting for the sweep, or until wait_timeout,
// when it answers with where the command stands. The timeout is bounded.
func TestCommandIssueWaits(t *testing.T) {
	r := newCommandStatusRig(t)
	srv := httptest.NewServer(api.NewHandler(r.gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: r.ctx, base: srv.URL}

	// other-1 reports nothing: a 2s window closes on no evidence, and the
	// wait answers timed out with the reason, without any sweep running.
	start := time.Now()
	timedOut := decodeCommand(t, c.do(r.ownerTok, http.MethodPost, "/components/other-1/commands:issue?wait=true&wait_timeout=10",
		map[string]any{"command_type": "set-input-soon", "value": "hdmi2"}, http.StatusOK))
	took := time.Since(start)
	if timedOut.Status != "timed-out" || timedOut.SettledBy != "window" || !strings.Contains(timedOut.Reason, "set-input-soon") {
		t.Fatalf("waited = status %q by %q reason %q, want timed out naming why", timedOut.Status, timedOut.SettledBy, timedOut.Reason)
	}
	if took < 2*time.Second || took > 8*time.Second {
		t.Fatalf("the wait took %v, want just past the 2s window", took)
	}

	// A wait shorter than the window answers with the command in flight.
	start = time.Now()
	inFlight := decodeCommand(t, c.do(r.ownerTok, http.MethodPost, "/components/other-1/commands:issue?wait=true&wait_timeout=1",
		map[string]any{"command_type": "set-input-later", "value": "hdmi2"}, http.StatusOK))
	if took := time.Since(start); took < time.Second || took > 4*time.Second {
		t.Fatalf("a 1s wait took %v", took)
	}
	if inFlight.Status != "issued" || inFlight.Phase != "awaiting-observation" {
		t.Fatalf("in flight = status %q phase %q, want issued, awaiting observation", inFlight.Status, inFlight.Phase)
	}

	for _, q := range []string{"wait=true&wait_timeout=0", "wait=true&wait_timeout=121"} {
		c.do(r.ownerTok, http.MethodPost, "/components/other-1/commands:issue?"+q, map[string]any{"command_type": "reboot"}, http.StatusUnprocessableEntity)
	}
}

// Waiting is bounded in how many may wait at once, in all and per caller: a
// wait past the limit is refused with a 429 before anything is issued, so a
// refused call has no side effect to retry around.
func TestCommandIssueWaitIsLimited(t *testing.T) {
	r := newCommandStatusRig(t)
	srv := httptest.NewServer(api.NewHandler(r.gw, api.WithCommandWaitLimits(2, 1)))
	defer srv.Close()
	c := &apiClient{t: t, ctx: r.ctx, base: srv.URL}
	second := principalWithGrants(t, r.ctx, r.dsn, "second-op", []grant{{role: "operator", scopeKind: "all"}})

	count := func() int {
		t.Helper()
		recs, err := r.gw.ListComponentCommands(r.ctx, r.componentID(t, "other-1"), storage.CommandFilter{})
		if err != nil {
			t.Fatal(err)
		}
		return len(recs)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		status, body := c.send(r.ownerTok, http.MethodPost, "/components/other-1/commands:issue?wait=true&wait_timeout=3",
			map[string]any{"command_type": "set-input-later", "value": "hdmi2"})
		if status != http.StatusOK {
			t.Errorf("the first wait = %d %s", status, body)
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for count() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	before := count()
	// The owner already waits once: its second wait is refused, and records
	// nothing; another caller may still wait.
	status, body := c.send(r.ownerTok, http.MethodPost, "/components/other-1/commands:issue?wait=true&wait_timeout=1",
		map[string]any{"command_type": "set-input-later", "value": "hdmi2"})
	if status != http.StatusTooManyRequests {
		t.Fatalf("a second wait by one caller = %d %s, want 429", status, body)
	}
	if n := count(); n != before {
		t.Fatalf("a refused wait recorded a command: %d commands, want %d", n, before)
	}
	c.do(second, http.MethodPost, "/components/other-1/commands:issue?wait=true&wait_timeout=1",
		map[string]any{"command_type": "set-input-later", "value": "hdmi2"}, http.StatusOK)
	// Without wait, an issue is never refused for the waiters.
	c.do(r.ownerTok, http.MethodPost, "/components/other-1/commands:issue", map[string]any{"command_type": "reboot"}, http.StatusOK)
	wg.Wait()
	// The slot is released when the wait ends.
	c.do(r.ownerTok, http.MethodPost, "/components/other-1/commands:issue?wait=true&wait_timeout=1",
		map[string]any{"command_type": "reboot"}, http.StatusOK)
}
