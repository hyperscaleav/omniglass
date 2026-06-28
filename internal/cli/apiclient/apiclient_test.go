package apiclient_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/cli/apiclient"
)

// TestDoAttachesAuthAndBody proves the client sends the bearer token, marshals a
// JSON body with the right content type, hits the joined path, and reports the
// server status without turning an HTTP error into a Go error.
func TestDoAttachesAuthAndBody(t *testing.T) {
	var gotAuth, gotCT, gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		gotPath = r.URL.Path
		gotMethod = r.Method
		if b, _ := io.ReadAll(r.Body); len(b) > 0 {
			_ = json.Unmarshal(b, &gotBody)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"x"}`))
	}))
	defer srv.Close()

	c := apiclient.New(srv.URL+"/", "tok-123") // trailing slash trimmed
	res, err := c.Do(context.Background(), http.MethodPost, "/api/v1/locations", map[string]any{"name": "hq"})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if res.Status != http.StatusCreated || !res.OK() {
		t.Errorf("status = %d, OK = %v, want 201 ok", res.Status, res.OK())
	}
	if string(res.Body) != `{"id":"x"}` {
		t.Errorf("body = %s, want the response echoed", res.Body)
	}
	if gotAuth != "Bearer tok-123" {
		t.Errorf("auth = %q, want Bearer tok-123", gotAuth)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q", gotCT)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/locations" {
		t.Errorf("got %s %s, want POST /api/v1/locations", gotMethod, gotPath)
	}
	if gotBody["name"] != "hq" {
		t.Errorf("body name = %v, want hq", gotBody["name"])
	}
}

// TestDoErrorStatusIsNotAGoError confirms a 404 is a Result, not an error, so the
// command layer can render the message and choose the exit code.
func TestDoErrorStatusIsNotAGoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"detail":"not found"}`))
	}))
	defer srv.Close()

	res, err := apiclient.New(srv.URL, "").Do(context.Background(), http.MethodGet, "/api/v1/locations/nope", nil)
	if err != nil {
		t.Fatalf("Do returned a Go error for a 404: %v", err)
	}
	if res.Status != http.StatusNotFound || res.OK() {
		t.Errorf("status = %d, OK = %v, want 404 not-ok", res.Status, res.OK())
	}
}

// TestDoOmitsAuthWhenTokenEmpty keeps an unauthenticated call header-clean (for
// healthz, which needs no token).
func TestDoOmitsAuthWhenTokenEmpty(t *testing.T) {
	var hadAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadAuth = r.Header["Authorization"]
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if _, err := apiclient.New(srv.URL, "").Do(context.Background(), http.MethodGet, "/api/v1/healthz", nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if hadAuth {
		t.Error("Authorization header sent with an empty token")
	}
}
