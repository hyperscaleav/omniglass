package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/sim/simtest"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// syncBuffer is a log sink the test reads while the server writes.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type webhookEndpoint struct {
	ID      string `json:"id"`
	Webhook *struct {
		Path    string `json:"path"`
		Scheme  string `json:"scheme"`
		Signed  bool   `json:"signed"`
		Secret  string `json:"secret"`
		Refused *struct {
			Count  int64  `json:"count"`
			Reason string `json:"reason"`
			Source string `json:"source"`
		} `json:"refused"`
	} `json:"webhook"`
}

// TestWebhookIngressEndToEnd drives the webhook transport over the real
// wire (#1024): the simulated UCC service, served in process, posts its
// signed meeting webhooks over HTTP to the real API, which checks each by
// the endpoint's scheme and lands it through the real bus into real rows. It
// walks the acceptance: a signed post lands its event, property and raw log
// line within a second and stamps last heard; a bad signature and a stale
// timestamp answer 401, land nothing and are counted; a redelivery lands
// once; an unknown, a rotated and a malformed token answer one identical
// 404 and the sender lands in the inbox, let go once its posts land; a
// missing credential refuses; a body past 1 MiB answers 413; a burst
// answers 429 with Retry-After while the rest of the API answers; and no
// token, key or signature reaches the server's log.
func TestWebhookIngressEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
	}
	logs := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	owner, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	all := scope.Set{All: true}
	for _, name := range []string{"ucc-1", "hook-2"} {
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name}, all, all, all, all); err != nil {
			t.Fatalf("create component: %v", err)
		}
	}
	busSrv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer busSrv.Shutdown()
	srv := httptest.NewServer(api.NewHandler(gw, api.WithTelemetryPublisher(busSrv)))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Configure the source: the signing key on the component, the endpoint
	// attaching the shipped ucc-webhook driver, its token revealed.
	const key = "ucc-e2e-signing-key"
	c.do(owner, http.MethodPost, "/secrets", map[string]any{
		"name": "ucc-signing", "secret_type": "hmac-key", "owner_kind": "component", "owner": "ucc-1",
		"fields": map[string]string{"key": key},
	}, http.StatusCreated)
	var ep webhookEndpoint
	mustJSON(t, c.do(owner, http.MethodPost, "/endpoints", map[string]any{
		"component": "ucc-1", "driver": "ucc-webhook", "inputs": map[string]string{"signing-key": "ucc-signing"},
	}, http.StatusCreated), &ep)
	if ep.Webhook == nil || ep.Webhook.Scheme != "hmac" || !ep.Webhook.Signed || !strings.HasSuffix(ep.Webhook.Secret, "/webhook-token") || strings.Contains(ep.Webhook.Path, "/webhooks/A") {
		t.Fatalf("endpoint webhook block = %+v, want hmac, signed, its secret named, the token masked", ep.Webhook)
	}
	reveal := func() string {
		var r struct {
			Fields map[string]string `json:"fields"`
		}
		mustJSON(t, c.do(owner, http.MethodPost, "/secrets/"+strings.ReplaceAll(ep.Webhook.Secret, "/", "%2F")+":reveal", nil, http.StatusOK), &r)
		return r.Fields["token"]
	}
	token := reveal()
	if len(token) != storage.WebhookTokenLen {
		t.Fatalf("revealed token = %d chars, want %d", len(token), storage.WebhookTokenLen)
	}
	hookURL := srv.URL + "/api/v1/webhooks/" + token

	// The simulated UCC service, subscribed to the endpoint's URL.
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatalf("load profiles: %v", err)
	}
	engine := sim.NewEngine(store, profiles.Templates())
	engine.SetPortMap(sim.EphemeralPorts)
	t.Cleanup(engine.StopAll)
	uccProfile := ""
	for name, p := range store.Devices {
		if p.Model == "UCC Platform" {
			uccProfile = name
		}
	}
	if err := engine.Sync([]sim.DeviceSnapshot{{DeviceID: "ucc", Hostname: "ucc", LoopbackIP: simtest.ClaimLoopback(t), ProfileName: uccProfile, Version: 1}}); err != nil {
		t.Fatalf("serve the ucc service: %v", err)
	}
	ucc := engine.Devices()[0]
	operate := func(path string, body any) {
		t.Helper()
		b, _ := json.Marshal(body)
		if res := ucc.Operate(sim.Operation{Kind: "http", Method: "POST", Path: path, Body: string(b)}); res.Status != 200 {
			t.Fatalf("POST %s on the ucc service = %+v", path, res)
		}
	}
	subscribe := func(fault string) {
		operate("/api/webhooks", map[string]string{"url": hookURL, "scheme": "hmac", "secret": key, "fault": fault})
	}
	events := func(meeting string) (started, ended int) {
		var out struct {
			Events []struct {
				Key        string            `json:"key"`
				Attributes map[string]string `json:"attributes"`
			} `json:"events"`
		}
		mustJSON(t, c.do(owner, http.MethodGet, "/components/ucc-1/events", nil, http.StatusOK), &out)
		for _, e := range out.Events {
			if e.Attributes["meeting"] != meeting {
				continue
			}
			switch e.Key {
			case "call-started":
				started++
			case "call-ended":
				ended++
			}
		}
		return started, ended
	}
	readEndpoint := func() webhookEndpoint {
		var e webhookEndpoint
		mustJSON(t, c.do(owner, http.MethodGet, "/endpoints/"+ep.ID, nil, http.StatusOK), &e)
		return e
	}

	// An unknown token first, so the inbox holds this sender.
	unknown := hookPost(t, srv.URL+"/api/v1/webhooks/AAAAAAAAAAAAAAAAAAAAAA", `{"event":"meeting.started"}`)
	if unknown.status != http.StatusNotFound {
		t.Fatalf("unknown token = %d, want 404", unknown.status)
	}
	waitFor(t, "the unknown sender in the inbox", func() bool {
		var out struct {
			Sources []struct {
				Host        string `json:"host"`
				Transport   string `json:"transport"`
				Source      string `json:"source"`
				LastMessage string `json:"last_message"`
				Count       int64  `json:"count"`
			} `json:"sources"`
		}
		_ = json.Unmarshal(c.do(owner, http.MethodGet, "/unclaimed-sources", nil, http.StatusOK), &out)
		return len(out.Sources) == 1 && out.Sources[0].Host == "server" && out.Sources[0].Transport == "webhook" &&
			out.Sources[0].Count == 1 && strings.Contains(out.Sources[0].LastMessage, "meeting.started")
	})

	// A signed post lands within a second: the event, the property, the raw
	// line, and last heard. Its landing claims the sender's inbox row.
	subscribe("")
	start := time.Now()
	operate("/api/meetings/start", map[string]string{"id": "m-1", "room": "huddle"})
	waitFor(t, "call-started for m-1", func() bool { s, _ := events("m-1"); return s == 1 })
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("the signed post took %s to land, want about a second", took)
	}
	waitFor(t, "call-state active", func() bool {
		s, err := gw.LatestProperty(ctx, "ucc-1", "call-state", "webhook")
		return err == nil && s != nil && strings.Contains(string(s.Value), "active")
	})
	waitFor(t, "the raw line", func() bool {
		var out struct {
			Logs []struct{ Message, Instance string } `json:"logs"`
		}
		_ = json.Unmarshal(c.do(owner, http.MethodGet, "/components/ucc-1/logs", nil, http.StatusOK), &out)
		return len(out.Logs) == 1 && out.Logs[0].Instance == "webhook" && strings.Contains(out.Logs[0].Message, `"meeting":"m-1"`)
	})
	waitFor(t, "last heard", func() bool {
		var out struct {
			Endpoints []struct {
				LastSeen *time.Time `json:"last_seen"`
				Webhook  *struct {
					Scheme string `json:"scheme"`
				} `json:"webhook"`
				Session *struct{} `json:"session"`
			} `json:"endpoints"`
		}
		_ = json.Unmarshal(c.do(owner, http.MethodGet, "/components/ucc-1/reachability", nil, http.StatusOK), &out)
		return len(out.Endpoints) == 1 && out.Endpoints[0].LastSeen != nil && out.Endpoints[0].Webhook != nil && out.Endpoints[0].Session == nil
	})
	var inbox struct {
		Sources []struct{} `json:"sources"`
	}
	mustJSON(t, c.do(owner, http.MethodGet, "/unclaimed-sources", nil, http.StatusOK), &inbox)
	if len(inbox.Sources) != 0 {
		t.Fatalf("inbox = %d sources after the sender's post landed, want it claimed", len(inbox.Sources))
	}
	// The inbox is fleet-wide, as nodes are: a viewer bounded to one
	// component reads none of it.
	uccComp, err := gw.GetComponent(ctx, "ucc-1", all)
	if err != nil {
		t.Fatalf("get component: %v", err)
	}
	scoped := principalWithGrants(t, ctx, dsn, "scoped-viewer", []grant{{role: "viewer", scopeKind: "component", scopeID: uccComp.ID}})
	c.do(scoped, http.MethodGet, "/unclaimed-sources", nil, http.StatusForbidden)

	// A wrong signature and a stale timestamp are refused, counted, and
	// land nothing.
	for _, f := range []struct{ fault, meeting, reason string }{
		{"bad-signature", "m-2", "signature mismatch"},
		{"stale", "m-3", "timestamp outside the window"},
	} {
		subscribe(f.fault)
		operate("/api/meetings/start", map[string]string{"id": f.meeting})
		waitFor(t, "the "+f.fault+" refusal counted", func() bool {
			e := readEndpoint()
			return e.Webhook.Refused != nil && e.Webhook.Refused.Reason == f.reason && e.Webhook.Refused.Source != ""
		})
		if s, _ := events(f.meeting); s != 0 {
			t.Fatalf("%s: landed %d call-started, want none", f.fault, s)
		}
	}

	// A redelivery lands once.
	subscribe("redeliver")
	operate("/api/meetings/start", map[string]string{"id": "m-4"})
	waitFor(t, "call-started for m-4", func() bool { s, _ := events("m-4"); return s == 1 })
	time.Sleep(500 * time.Millisecond)
	if s, _ := events("m-4"); s != 1 {
		t.Fatalf("a redelivered meeting landed %d times, want once", s)
	}

	// The meeting ends: call-ended and call-state idle.
	subscribe("")
	operate("/api/meetings/end", map[string]string{})
	waitFor(t, "call-ended for m-4", func() bool { _, e := events("m-4"); return e == 1 })

	// Rotated: the old token answers exactly what an unknown one does, and
	// so does a malformed one.
	c.do(owner, http.MethodPost, "/endpoints/"+ep.ID+":rotateToken", nil, http.StatusOK)
	rotated := hookPost(t, hookURL, `{"event":"meeting.started"}`)
	malformed := hookPost(t, srv.URL+"/api/v1/webhooks/not-a-token", `{}`)
	for name, r := range map[string]reply{"rotated": rotated, "malformed": malformed} {
		if r.status != unknown.status || !bytes.Equal(r.body, unknown.body) {
			t.Fatalf("%s token = %d %s, want the unknown token's %d %s byte for byte", name, r.status, r.body, unknown.status, unknown.body)
		}
	}
	newToken := reveal()
	if newToken == token {
		t.Fatal("rotation kept the token")
	}
	hookURL = srv.URL + "/api/v1/webhooks/" + newToken
	subscribe("")
	operate("/api/meetings/start", map[string]string{"id": "m-5"})
	waitFor(t, "call-started for m-5 at the new token", func() bool { s, _ := events("m-5"); return s == 1 })

	// A credential that no longer resolves refuses every post.
	c.do(owner, http.MethodDelete, "/secrets/ucc-1%2Fucc-signing", nil, http.StatusNoContent)
	operate("/api/meetings/start", map[string]string{"id": "m-6"})
	waitFor(t, "the missing credential counted", func() bool {
		e := readEndpoint()
		return e.Webhook.Refused != nil && e.Webhook.Refused.Reason == "the endpoint's credential is missing"
	})
	if s, _ := events("m-6"); s != 0 {
		t.Fatal("a post to an endpoint whose key is gone landed")
	}

	// A bare webhook endpoint lands every post as a log line, unsigned; a
	// GET answers 204, its driver declaring no challenge.
	var bare webhookEndpoint
	mustJSON(t, c.do(owner, http.MethodPost, "/endpoints", map[string]any{"component": "hook-2", "transport": "webhook"}, http.StatusCreated), &bare)
	if bare.Webhook == nil || bare.Webhook.Scheme != "none" || bare.Webhook.Signed {
		t.Fatalf("bare webhook block = %+v, want scheme none, unsigned", bare.Webhook)
	}
	c.do(owner, http.MethodPost, "/endpoints", map[string]any{"component": "hook-2", "transport": "webhook", "node": "anywhere"}, http.StatusUnprocessableEntity)
	var bareTok struct {
		Fields map[string]string `json:"fields"`
	}
	mustJSON(t, c.do(owner, http.MethodPost, "/secrets/"+strings.ReplaceAll(bare.Webhook.Secret, "/", "%2F")+":reveal", nil, http.StatusOK), &bareTok)
	bareURL := srv.URL + "/api/v1/webhooks/" + bareTok.Fields["token"]
	if r := hookPost(t, bareURL, "room=huddle&state=idle"); r.status != http.StatusAccepted {
		t.Fatalf("bare post = %d %s, want 202", r.status, r.body)
	}
	waitFor(t, "the bare post as a log line", func() bool {
		var out struct {
			Logs []struct{ Message string } `json:"logs"`
		}
		_ = json.Unmarshal(c.do(owner, http.MethodGet, "/components/hook-2/logs", nil, http.StatusOK), &out)
		return len(out.Logs) == 1 && out.Logs[0].Message == "room=huddle&state=idle"
	})
	if r := hookGet(t, bareURL); r.status != http.StatusNoContent {
		t.Fatalf("verifying a known url = %d, want 204", r.status)
	}

	// A body past 1 MiB answers 413.
	if r := hookPost(t, bareURL, strings.Repeat("x", 1<<20+1)); r.status != http.StatusRequestEntityTooLarge {
		t.Fatalf("an oversized post = %d, want 413", r.status)
	}

	// No token, key or signature reached the server's log.
	for _, secret := range []string{token, newToken, key, bareTok.Fields["token"]} {
		if i := strings.Index(logs.String(), secret); i >= 0 {
			line := logs.String()[max(0, i-300):i]
			t.Fatalf("the server's log carries a credential, after: %s", line)
		}
	}

	// A burst past the source's bucket answers 429 with Retry-After, while
	// the rest of the API keeps answering.
	limited := false
	for i := 0; i < 60 && !limited; i++ {
		r := hookPost(t, srv.URL+"/api/v1/webhooks/AAAAAAAAAAAAAAAAAAAAAA", `{}`)
		if r.status == http.StatusTooManyRequests {
			limited = r.retryAfter != ""
		}
	}
	if !limited {
		t.Fatal("a burst was never answered 429 with Retry-After")
	}
	c.do("", http.MethodGet, "/healthz", nil, http.StatusOK)
}

type reply struct {
	status     int
	body       []byte
	retryAfter string
}

func hookPost(t *testing.T, url, body string) reply {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return reply{status: resp.StatusCode, body: b, retryAfter: resp.Header.Get("Retry-After")}
}

func hookGet(t *testing.T, url string) reply {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return reply{status: resp.StatusCode, body: b}
}
