package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestPasswordLoginCookieSession drives the whole login path against the real
// binary: a wrong password is 401, a right one sets an httpOnly session cookie
// that authenticates /auth/me, no cookie is 401, and logout revokes the token so
// the same cookie stops working. Skipped under -short.
func TestPasswordLoginCookieSession(t *testing.T) {
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

	pwHash, err := auth.HashPassword("s3cret-pw")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	_, bh, bp, _ := auth.NewBearerToken()
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{
		Username: "ops", SecretHash: bh, Prefix: bp, PasswordHash: pwHash,
	}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()

	login := func(user, pw string) *http.Response {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"username": user, "password": pw})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/api/v1/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("login request: %v", err)
		}
		return resp
	}
	me := func(c *http.Cookie) (int, []byte) {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/auth/me", nil)
		if c != nil {
			req.AddCookie(c)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("me request: %v", err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b
	}

	// Wrong password: 401, no cookie.
	if bad := login("ops", "nope"); bad.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password: want 401, got %d", bad.StatusCode)
	} else {
		bad.Body.Close()
	}

	// Right password: 204 plus an httpOnly session cookie.
	ok := login("ops", "s3cret-pw")
	if ok.StatusCode != http.StatusNoContent {
		t.Fatalf("login: want 204, got %d", ok.StatusCode)
	}
	var session *http.Cookie
	for _, c := range ok.Cookies() {
		if c.Name == "og_session" {
			session = c
		}
	}
	ok.Body.Close()
	if session == nil || session.Value == "" {
		t.Fatal("expected an og_session cookie")
	}
	if !session.HttpOnly {
		t.Fatal("the session cookie must be HttpOnly")
	}

	// The cookie authenticates /auth/me; no cookie is 401.
	if code, body := me(session); code != http.StatusOK || !bytes.Contains(body, []byte(`"username":"ops"`)) {
		t.Fatalf("me via cookie: want 200 with ops, got %d (%s)", code, body)
	}
	if code, _ := me(nil); code != http.StatusUnauthorized {
		t.Fatalf("me without cookie: want 401, got %d", code)
	}

	// A stale or invalid bearer header must not shadow a valid session cookie: the
	// authn middleware falls through to the cookie.
	stale, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/auth/me", nil)
	stale.AddCookie(session)
	stale.Header.Set("Authorization", "Bearer ogp_deadbeef_notarealtoken")
	staleResp, err := http.DefaultClient.Do(stale)
	if err != nil {
		t.Fatalf("me with stale bearer + cookie: %v", err)
	}
	staleResp.Body.Close()
	if staleResp.StatusCode != http.StatusOK {
		t.Fatalf("a stale bearer should fall through to the cookie: want 200, got %d", staleResp.StatusCode)
	}

	// Logout revokes the session token: the same cookie no longer authenticates.
	lreq, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/api/v1/auth/logout", nil)
	lreq.AddCookie(session)
	lresp, err := http.DefaultClient.Do(lreq)
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	lresp.Body.Close()
	if code, _ := me(session); code != http.StatusUnauthorized {
		t.Fatalf("me after logout: want 401, got %d", code)
	}
}

// TestAuthStatus proves the public /auth/status flips from not-bootstrapped to
// bootstrapped when the first owner is created, with no auth required (it drives
// the login screen's bootstrap hint). Skipped under -short.
func TestAuthStatus(t *testing.T) {
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

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()

	status := func() bool {
		t.Helper()
		resp, err := http.DefaultClient.Get(srv.URL + "/api/v1/auth/status")
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status code: want 200, got %d", resp.StatusCode)
		}
		var body struct {
			Bootstrapped bool `json:"bootstrapped"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return body.Bootstrapped
	}

	if status() {
		t.Fatal("a fresh system should report not bootstrapped")
	}
	_, bh, bp, _ := auth.NewBearerToken()
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "ops", SecretHash: bh, Prefix: bp}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !status() {
		t.Fatal("after bootstrap the system should report bootstrapped")
	}
}
