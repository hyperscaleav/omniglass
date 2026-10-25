package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestReachabilityServesHTTPGatesWithTheirReasonAndKeys drives the read the
// console's refusal notice renders for http endpoints (#902 on #1110): a
// credentialed request's 401 gates an http endpoint at the session rung, and
// a changed certificate key gates an https one at the transport rung; neither
// holds a session. Each serves its rung, class, the node's reason and the
// gate's publish time, so the notice says why and offers Retry; the https
// gate also carries the pinned and offered keys, which is what Trust this key
// renders and sends, and the server takes the offered key as certificate. A
// viewer reads the same gates and its retry is refused. Skipped under -short.
func TestReachabilityServesHTTPGatesWithTheirReasonAndKeys(t *testing.T) {
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
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	comp := "bar-1"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: comp}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	for tr, params := range map[string]string{"http": `{"target":"127.60.0.9:80"}`, "https": `{"target":"127.60.0.9:443","tls-mode":"pin"}`} {
		if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: tr, Component: &comp, Params: []byte(params)}, all); err != nil {
			t.Fatalf("%s endpoint: %v", tr, err)
		}
	}
	const fingerprint = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	pinned, offered := "SHA256:"+strings.Repeat("A", 43), "SHA256:"+strings.Repeat("B", 43)
	authReason := "auth: the component refused the credential (401)"
	certReason := "certificate: the certificate's public key changed: pinned " + pinned + ", offered " + offered + "; retry the endpoint with the offered key to trust it"
	at := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Microsecond)
	prop := func(instance, key, value string) storage.PropertySampleWrite {
		return storage.PropertySampleWrite{OwnerKind: "component", OwnerID: comp, Key: key, Instance: instance, Value: value, Source: instance, TS: at}
	}
	if err := insertProperties(ctx, gw, []storage.PropertySampleWrite{
		prop("http", "endpoint-rejection", "authenticated auth "+fingerprint),
		prop("http", "endpoint-rejection-reason", authReason),
		prop("https", "endpoint-rejection", "responsive certificate "+fingerprint+" "+pinned+" "+offered),
		prop("https", "endpoint-rejection-reason", certReason),
	}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	type gateResp struct {
		Endpoints []struct {
			ID        string    `json:"id"`
			Endpoint  string    `json:"endpoint"`
			Session   *struct{} `json:"session"`
			Rejection *struct {
				Rung       string    `json:"rung"`
				Class      string    `json:"class"`
				Reason     string    `json:"reason"`
				TS         time.Time `json:"ts"`
				PinnedKey  string    `json:"pinned_key"`
				OfferedKey string    `json:"offered_key"`
			} `json:"rejection"`
		} `json:"endpoints"`
	}
	read := func(tok string) map[string]string {
		t.Helper()
		out := c.do(tok, http.MethodGet, "/components/"+comp+"/reachability", nil, http.StatusOK)
		if strings.Contains(string(out), fingerprint) {
			t.Fatalf("the read served the connection fingerprint: %s", out)
		}
		var r gateResp
		if err := json.Unmarshal(out, &r); err != nil {
			t.Fatal(err)
		}
		ids := map[string]string{}
		for _, e := range r.Endpoints {
			ids[e.Endpoint] = e.ID
			g := e.Rejection
			if e.Session != nil || g == nil || !g.TS.Equal(at) {
				t.Fatalf("%s: session %v, gate %+v, want a gate published at %v and no session", e.Endpoint, e.Session, g, at)
			}
			switch e.Endpoint {
			case "http":
				if g.Rung != "authenticated" || g.Class != "auth" || g.Reason != authReason || g.OfferedKey != "" {
					t.Fatalf("the http gate = %+v", g)
				}
			case "https":
				if g.Rung != "responsive" || g.Class != "certificate" || g.Reason != certReason || g.PinnedKey != pinned || g.OfferedKey != offered {
					t.Fatalf("the https gate = %+v", g)
				}
			}
		}
		if ids["http"] == "" || ids["https"] == "" {
			t.Fatalf("endpoints read: %v", ids)
		}
		return ids
	}
	ids := read(ownerTok)
	viewerTok := setupAllViewer(t, ctx, dsn, "viewer-http")
	read(viewerTok)

	// Retry for the refused credential, Trust this key for the certificate:
	// a viewer is refused both, the owner's land, and a key the refusal does
	// not name is refused (409).
	c.do(viewerTok, http.MethodPost, "/endpoints/"+ids["http"]+":retry", nil, http.StatusForbidden)
	c.do(viewerTok, http.MethodPost, "/endpoints/"+ids["https"]+":retry", map[string]any{"certificate": offered}, http.StatusForbidden)
	c.do(ownerTok, http.MethodPost, "/endpoints/"+ids["https"]+":retry", map[string]any{"certificate": "SHA256:" + strings.Repeat("C", 43)}, http.StatusConflict)
	c.do(ownerTok, http.MethodPost, "/endpoints/"+ids["http"]+":retry", nil, http.StatusOK)
	c.do(ownerTok, http.MethodPost, "/endpoints/"+ids["https"]+":retry", map[string]any{"certificate": offered}, http.StatusOK)
	// The console's Trust this key sends the offered key as host_key, the
	// field every key refusal's trust uses; an https endpoint takes it too.
	c.do(ownerTok, http.MethodPost, "/endpoints/"+ids["https"]+":retry", map[string]any{"host_key": offered}, http.StatusOK)
}
