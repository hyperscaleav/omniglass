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

// TestASessionRecordsTheClientTheTrustedProxiesResolve is #1185's end-to-end
// proof: the address a login stamps on its session is the one the configured
// trusted-proxy list resolves, the same answer the webhook receiver limits by.
// The test client reaches the server over loopback, so loopback is the peer.
// Under the default list (loopback trusted) the rightmost untrusted forwarded
// hop is recorded, never the spoofable leftmost one; under "none" a forwarded
// header changes nothing. Skipped under -short.
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

	recorded := func(t *testing.T, opts ...api.Option) string {
		t.Helper()
		srv := httptest.NewServer(api.NewHandler(gw, opts...))
		defer srv.Close()
		b, _ := json.Marshal(map[string]string{"username": "ops", "password": "orange-boat-42x"})
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/auth/login", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", "203.0.113.66, 198.51.100.7")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("login: want 204, got %d", resp.StatusCode)
		}
		var cookie *http.Cookie
		for _, c := range resp.Cookies() {
			if c.Name == "og-session" {
				cookie = c
			}
		}
		if cookie == nil {
			t.Fatal("login: no session cookie")
		}
		req, _ = http.NewRequest(http.MethodGet, srv.URL+"/api/v1/auth/me/sessions", nil)
		req.AddCookie(cookie)
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("list sessions: %v", err)
		}
		defer resp.Body.Close()
		var body struct {
			Sessions []struct {
				ClientIP string `json:"client_ip"`
				Current  bool   `json:"current"`
			} `json:"sessions"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
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

	t.Run("the default trusts the loopback proxy and takes its rightmost untrusted hop", func(t *testing.T) {
		if got := recorded(t); got != "198.51.100.7" {
			t.Fatalf("client_ip = %q, want 198.51.100.7 (never the leftmost 203.0.113.66)", got)
		}
	})
	t.Run("none believes no forwarded header", func(t *testing.T) {
		if got := recorded(t, api.WithTrustedProxies(ingress.TrustedProxies{})); got != "127.0.0.1" {
			t.Fatalf("client_ip = %q, want the loopback peer 127.0.0.1", got)
		}
	})
}

// TestAWebhookSenderIsTheClientTheTrustedProxiesResolve: the webhook
// receiver reads the same configured list, so a stranger's post lands in the
// unclaimed-sources inbox under the address a credential would be stamped
// with. Skipped under -short.
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
	inboxHas := func(t *testing.T, opts []api.Option, want string) {
		t.Helper()
		srv := httptest.NewServer(api.NewHandler(gw, append(opts, api.WithTelemetryPublisher(discardPublisher{}))...))
		defer srv.Close()
		c := &apiClient{t: t, ctx: ctx, base: srv.URL}
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/webhooks/AAAAAAAAAAAAAAAAAAAAAA", strings.NewReader(`{"event":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", "203.0.113.66, 198.51.100.7")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("unknown token = %d, want 404", resp.StatusCode)
		}
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
	t.Run("the default resolves the forwarded client", func(t *testing.T) {
		inboxHas(t, nil, "198.51.100.7")
	})
	t.Run("none records the loopback peer", func(t *testing.T) {
		inboxHas(t, []api.Option{api.WithTrustedProxies(ingress.TrustedProxies{})}, "127.0.0.1")
	})
}

// discardPublisher stands in for the bus where nothing reaches the ingest
// lane: the receiver needs one to answer at all.
type discardPublisher struct{}

func (discardPublisher) PublishTelemetry(context.Context, *ogv1.TelemetryBatch) error { return nil }
