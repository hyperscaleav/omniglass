package api_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/egress"
	"github.com/hyperscaleav/omniglass/internal/lab"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/sso"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The smoke test against a real identity provider (#2362), per the
// capability-wrapping rule: Dex in a testcontainer, run from the lab's own
// configuration, its local password connector signing the lab's demo person
// in through its real login form (linked here to alice), and a re-check
// redeeming the refresh token Dex issued. The fake provider proves the
// refusals; this proves the flow interoperates with a provider we did not
// write, and that the identity the lab's seed links is the one Dex vouches
// for. Skipped under -short.

func TestSSOSignsInThroughARealDexProvider(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Docker")
	}
	ctx := context.Background()
	dsn := seededDSN(t)
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	logs := &lockedBuffer{}
	srv := httptest.NewServer(api.NewHandler(gw, api.WithVersion("test"), api.WithEgressOptions(egress.Options{}),
		api.WithLogger(slog.New(slog.NewJSONHandler(logs, nil)))))
	t.Cleanup(srv.Close)
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	owner := bootstrapOwnerTok(t, ctx, gw)

	// Dex learns its issuer from the port it is mapped to, so it waits for its
	// config, which is written once the port is known.
	ctr, err := testcontainers.Run(ctx, lab.DexImage,
		testcontainers.WithExposedPorts("5556/tcp"),
		testcontainers.WithEntrypoint("sh", "-c", "until [ -f /tmp/dex.yaml ]; do sleep 0.1; done; exec dex serve /tmp/dex.yaml"),
	)
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("start dex: %v", err)
	}
	port, err := ctr.MappedPort(ctx, "5556/tcp")
	if err != nil {
		t.Fatal(err)
	}
	issuer := fmt.Sprintf("http://127.0.0.1:%s/dex", port.Port())
	// The lab's own Dex configuration (internal/lab/sso.go), on this issuer:
	// what a walkthrough lab runs is what this signs in through.
	cfg := lab.DexConfig(issuer, srv.URL+sso.CallbackPath)
	if err := ctr.CopyToContainer(ctx, []byte(cfg), "/tmp/dex.yaml", 0o644); err != nil {
		t.Fatalf("write dex config: %v", err)
	}
	waitForDiscovery(t, issuer)

	// Dex listens on plain http on loopback: the allow list names it, which
	// is the only way the egress contract reaches either.
	c.do(owner, http.MethodPatch, "/settings/notification", map[string]any{"egress_allow": []string{"127.0.0.1"}}, http.StatusOK)
	c.do(owner, http.MethodPost, "/secrets", map[string]any{"name": "dex-client", "secret_type": "oauth2-client", "owner_kind": "platform",
		"fields": map[string]string{"client_id": lab.DexClientID, "client_secret": lab.DexClientSecret}}, http.StatusCreated)
	c.do(owner, http.MethodPatch, "/settings/sso", map[string]any{"issuer": issuer, "client_secret": "dex-client"}, http.StatusOK)
	s := &ssoStack{c: c, ctx: ctx, base: srv.URL, owner: owner, dsn: dsn, gw: gw}
	alice := s.person(t, "alice")

	// Unlinked, the page gets the code and the server's log names the
	// identity Dex vouched for; an admin links exactly that, and the next
	// sign-in lands alice in the console.
	if code := ssoError(t, dexSignIn(t, srv.URL)); code != string(sso.ErrUnlinked) {
		t.Fatalf("unlinked sign-in: sso_error = %q, want %s", code, sso.ErrUnlinked)
	}
	// It is the identity the lab's seed links for its demo person (on the
	// lab's issuer), so a walkthrough lab's sign-in finds its link.
	ident, _ := lastRefusal(t, logs)["identity"].(string)
	if want := lab.DexDemoIdentity(issuer); ident != want {
		t.Fatalf("unlinked sign-in: logged identity %q, want the lab's demo identity %s", ident, want)
	}
	c.do(owner, http.MethodPost, "/users/"+alice+"/identity-links", map[string]string{"identifier": ident}, http.StatusCreated)
	jar := dexJar(t)
	if to := dexSignInWith(t, srv.URL, jar); to != srv.URL+"/web/" {
		t.Fatalf("linked sign-in landed on %q", to)
	}
	base, _ := url.Parse(srv.URL)
	var sess string
	for _, ck := range jar.Cookies(base) {
		if ck.Name == "og-session" {
			sess = ck.Value
		}
	}
	if code, who := s.username(t, sess); code != http.StatusOK || who != "alice" {
		t.Fatalf("auth/me through Dex = %d %q", code, who)
	}

	// Dex issued a refresh token (offline_access): the re-check redeems it.
	p, _, err := sso.NewSweeper(gw, egress.Options{}, nil).RunOnce(ctx)
	if err != nil || p.Accepted != 1 {
		t.Fatalf("re-check against Dex = %+v, %v; want one accepted", p, err)
	}
}

func waitForDiscovery(t *testing.T, issuer string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(issuer + "/.well-known/openid-configuration")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("dex never served discovery at %s", issuer)
}

func dexJar(t *testing.T) *cookiejar.Jar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return jar
}

func dexSignIn(t *testing.T, base string) string { return dexSignInWith(t, base, dexJar(t)) }

// dexSignInWith drives a browser from Omniglass's start through Dex's login
// form back to the callback, returning where the callback sends it.
func dexSignInWith(t *testing.T, base string, jar *cookiejar.Jar) string {
	t.Helper()
	hc := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	next := base + "/auth/oidc/start/default"
	posted := false
	for range 12 {
		resp, err := hc.Get(next)
		if err != nil {
			t.Fatalf("GET %s: %v", next, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK && !posted && bytes.Contains(body, []byte(`name="password"`)) {
			// Dex's login form posts back to the page it is on.
			resp, err = hc.PostForm(next, url.Values{"login": {lab.SSODemoEmail}, "password": {lab.SSODemoPassword}})
			if err != nil {
				t.Fatalf("post dex login: %v", err)
			}
			_ = resp.Body.Close()
			posted = true
		}
		loc := resp.Header.Get("Location")
		if loc == "" {
			t.Fatalf("dex flow stopped at %s with %d", next, resp.StatusCode)
		}
		u, err := resp.Request.URL.Parse(loc)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(u.String(), base+"/web/") {
			return u.String()
		}
		next = u.String()
	}
	t.Fatalf("dex flow did not return to Omniglass")
	return ""
}
