package api_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"golang.org/x/crypto/bcrypt"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/egress"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/sso"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The smoke test against a real identity provider (#2362), per the
// capability-wrapping rule: Dex in a testcontainer, its local password
// connector signing alice in through its real login form, and a re-check
// redeeming the refresh token Dex issued. The fake provider proves the
// refusals; this proves the flow interoperates with a provider we did not
// write. Skipped under -short.

const dexImage = "ghcr.io/dexidp/dex:v2.44.0"

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
	srv := httptest.NewServer(api.NewHandler(gw, api.WithVersion("test"), api.WithEgressOptions(egress.Options{})))
	t.Cleanup(srv.Close)
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	owner := bootstrapOwnerTok(t, ctx, gw)

	// Dex learns its issuer from the port it is mapped to, so it waits for its
	// config, which is written once the port is known.
	ctr, err := testcontainers.Run(ctx, dexImage,
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
	hash, err := bcrypt.GenerateFromPassword([]byte("dex-password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`issuer: %s
storage: {type: memory}
web: {http: 0.0.0.0:5556}
oauth2: {skipApprovalScreen: true}
enablePasswordDB: true
staticPasswords:
  - email: alice@example.com
    hash: %q
    username: alice
    userID: 6a1b2c3d-0000-4000-8000-000000000001
staticClients:
  - id: omniglass
    secret: dex-client-secret
    name: Omniglass
    redirectURIs: [%q]
`, issuer, string(hash), srv.URL+sso.CallbackPath)
	if err := ctr.CopyToContainer(ctx, []byte(cfg), "/tmp/dex.yaml", 0o644); err != nil {
		t.Fatalf("write dex config: %v", err)
	}
	waitForDiscovery(t, issuer)

	// Dex listens on plain http on loopback: the allow list names it, which
	// is the only way the egress contract reaches either.
	c.do(owner, http.MethodPatch, "/settings/notification", map[string]any{"egress_allow": []string{"127.0.0.1"}}, http.StatusOK)
	c.do(owner, http.MethodPost, "/secrets", map[string]any{"name": "dex-client", "secret_type": "oauth2-client", "owner_kind": "platform",
		"fields": map[string]string{"client_id": "omniglass", "client_secret": "dex-client-secret"}}, http.StatusCreated)
	c.do(owner, http.MethodPatch, "/settings/sso", map[string]any{"issuer": issuer, "client_secret": "dex-client"}, http.StatusOK)
	s := &ssoStack{c: c, ctx: ctx, base: srv.URL, owner: owner, dsn: dsn, gw: gw}
	alice := s.person(t, "alice")

	// Unlinked, the refusal names the identity Dex vouched for; an admin links
	// exactly that, and the next sign-in lands alice in the console.
	msg := ssoError(t, dexSignIn(t, srv.URL))
	_, rest, ok := strings.Cut(msg, "(")
	ident, _, _ := strings.Cut(rest, ")")
	if !ok || !strings.HasPrefix(ident, issuer+"|") {
		t.Fatalf("unlinked sign-in: sso_error = %q, want it to name %s|<sub>", msg, issuer)
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
			resp, err = hc.PostForm(next, url.Values{"login": {"alice@example.com"}, "password": {"dex-password"}})
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
