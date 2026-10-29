package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/ingress"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
)

// trustedProxies parses a list in OMNIGLASS_TRUSTED_PROXIES' grammar, the way
// the server does at boot.
func trustedProxies(t *testing.T, s string) ingress.TrustedProxies {
	t.Helper()
	tp, err := ingress.ParseTrustedProxies(s)
	if err != nil {
		t.Fatalf("ParseTrustedProxies(%q): %v", s, err)
	}
	return tp
}

// TestASessionRecordsTheClientTheTrustedProxiesResolve is #1185's end-to-end
// proof: the address a login stamps on its session is the one the trusted
// proxies resolve. By default only a loopback peer is believed, so a client
// reaching the server from anywhere else is recorded as itself whatever its
// X-Forwarded-For claims; a configured proxy's chain is read from the right,
// past every trusted hop, never its spoofable leftmost hop; and every header
// line is read, in order. Each login is served with the peer the case names,
// as the listener would set it. Skipped under -short.
func TestASessionRecordsTheClientTheTrustedProxiesResolve(t *testing.T) {
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
	pwHash, err := auth.HashPassword("orange-boat-42x")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	_, bh, bp, _ := auth.NewBearerToken()
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "ops", SecretHash: bh, Prefix: bp, PasswordHash: pwHash}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	// recorded logs in from peer, carrying each forwarded line as its own
	// X-Forwarded-For header line, and returns the client_ip the new session
	// was stamped with.
	recorded := func(t *testing.T, opts []api.Option, peer string, forwarded ...string) string {
		t.Helper()
		h := api.NewHandler(gw, opts...)
		b, _ := json.Marshal(map[string]string{"username": "ops", "password": "orange-boat-42x"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(b))
		req.RemoteAddr = peer
		req.Header.Set("Content-Type", "application/json")
		for _, line := range forwarded {
			req.Header.Add("X-Forwarded-For", line)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("login: want 204, got %d: %s", rec.Code, rec.Body)
		}
		var cookie *http.Cookie
		for _, c := range rec.Result().Cookies() {
			if c.Name == "og-session" {
				cookie = c
			}
		}
		if cookie == nil {
			t.Fatal("login: no session cookie")
		}
		req = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me/sessions", nil)
		req.AddCookie(cookie)
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var body struct {
			Sessions []struct {
				ClientIP string `json:"client_ip"`
				Current  bool   `json:"current"`
			} `json:"sessions"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode sessions: %v", err)
		}
		for _, s := range body.Sessions {
			if s.Current {
				return s.ClientIP
			}
		}
		t.Fatal("no current session listed")
		return ""
	}

	pods := []api.Option{api.WithTrustedProxies(trustedProxies(t, "10.42.0.0/16"))}
	none := []api.Option{api.WithTrustedProxies(trustedProxies(t, "none"))}
	cases := []struct {
		name      string
		opts      []api.Option
		peer      string
		forwarded []string
		want      string
	}{
		{"a non-loopback peer's forwarded header is ignored by default",
			nil, "10.42.0.7:51234", []string{"203.0.113.66, 198.51.100.7"}, "10.42.0.7"},
		{"a public peer's forwarded header is ignored by default",
			nil, "203.0.113.9:51234", []string{"198.51.100.7"}, "203.0.113.9"},
		{"a loopback proxy is believed by default, read from the right",
			nil, "127.0.0.1:51234", []string{"203.0.113.66, 198.51.100.7"}, "198.51.100.7"},
		{"a configured proxy's chain is read past every trusted hop",
			pods, "10.42.0.7:51234", []string{"203.0.113.66, 198.51.100.7, 10.42.3.9"}, "198.51.100.7"},
		{"a configured list replaces the default whole",
			pods, "127.0.0.1:51234", []string{"198.51.100.7"}, "127.0.0.1"},
		{"a line a proxy appends after the client's own is read last",
			nil, "127.0.0.1:51234", []string{"203.0.113.66", "198.51.100.7"}, "198.51.100.7"},
		{"the walk crosses header lines",
			pods, "10.42.0.7:51234", []string{"203.0.113.66, 198.51.100.7", "10.42.3.9"}, "198.51.100.7"},
		{"an empty header line changes nothing",
			pods, "10.42.0.7:51234", []string{"198.51.100.7", ""}, "198.51.100.7"},
		{"none believes no forwarded header, not even loopback's",
			none, "127.0.0.1:51234", []string{"198.51.100.7"}, "127.0.0.1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := recorded(t, c.opts, c.peer, c.forwarded...); got != c.want {
				t.Fatalf("client_ip = %q, want %q (peer %s, X-Forwarded-For %q)", got, c.want, c.peer, c.forwarded)
			}
		})
	}
}

// TestAWebhookSenderIsTheClientTheTrustedProxiesResolve: the webhook
// receiver reads the same resolved client, so a stranger's post lands in the
// unclaimed-sources inbox under the address a credential would be stamped
// with. The loopback cases go over a real socket; the non-loopback one is
// served with that peer, as the listener would set it. Skipped under -short.
func TestAWebhookSenderIsTheClientTheTrustedProxiesResolve(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	owner, hash, prefix, _ := auth.NewBearerToken()
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	const forged = "203.0.113.66, 198.51.100.7"
	post := func(t *testing.T, h http.Handler, peer string) {
		t.Helper()
		var code int
		if peer == "" {
			srv := httptest.NewServer(h)
			defer srv.Close()
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/webhooks/AAAAAAAAAAAAAAAAAAAAAA", strings.NewReader(`{"event":"x"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Forwarded-For", forged)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("post: %v", err)
			}
			resp.Body.Close()
			code = resp.StatusCode
		} else {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/AAAAAAAAAAAAAAAAAAAAAA", strings.NewReader(`{"event":"x"}`))
			req.RemoteAddr = peer
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Forwarded-For", forged)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			code = rec.Code
		}
		if code != http.StatusNotFound {
			t.Fatalf("unknown token = %d, want 404", code)
		}
	}
	inboxHas := func(t *testing.T, opts []api.Option, peer, want string) {
		t.Helper()
		h := api.NewHandler(gw, append(opts, api.WithTelemetryPublisher(discardPublisher{}))...)
		post(t, h, peer)
		srv := httptest.NewServer(h)
		defer srv.Close()
		c := &apiClient{t: t, ctx: ctx, base: srv.URL}
		waitFor(t, "the sender "+want+" in the inbox", func() bool {
			var out struct {
				Sources []struct {
					Source string `json:"source"`
				} `json:"sources"`
			}
			mustJSON(t, c.do(owner, http.MethodGet, "/unclaimed-sources", nil, http.StatusOK), &out)
			for _, s := range out.Sources {
				if s.Source == want {
					return true
				}
			}
			return false
		})
	}
	t.Run("the default believes a loopback proxy", func(t *testing.T) {
		inboxHas(t, nil, "", "198.51.100.7")
	})
	t.Run("the default ignores a non-loopback peer's forwarded header", func(t *testing.T) {
		inboxHas(t, nil, "10.42.0.7:51234", "10.42.0.7")
	})
	t.Run("none records the loopback peer", func(t *testing.T) {
		inboxHas(t, []api.Option{api.WithTrustedProxies(ingress.TrustedProxies{})}, "", "127.0.0.1")
	})
}

// discardPublisher stands in for the bus where nothing reaches the ingest
// lane: the receiver needs one to answer at all.
type discardPublisher struct{}

func (discardPublisher) PublishTelemetry(context.Context, *ogv1.TelemetryBatch) error { return nil }
