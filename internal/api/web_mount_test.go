package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// webStubGateway satisfies the Gateway interface with no backend; the /web mount
// never touches it.
type webStubGateway struct{ storage.UnimplementedGateway }

// TestWebConsoleMount proves the SPA is mounted under /web on the API handler.
// Untagged (the test build), no console is embedded, so the handler serves the
// build-the-console placeholder; the contract under test is the routing (a 200
// HTML shell under /web/, the bare /web redirect), which holds identically once
// the real console is embedded with -tags web.
func TestWebConsoleMount(t *testing.T) {
	srv := httptest.NewServer(api.NewHandler(webStubGateway{}))
	defer srv.Close()

	// No-redirect client so we can assert the /web -> /web/ redirect.
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := noRedirect.Get(srv.URL + "/web")
	if err != nil {
		t.Fatalf("get /web: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMovedPermanently {
		t.Errorf("GET /web = %d, want 301", resp.StatusCode)
	}

	resp2, err := http.Get(srv.URL + "/web/")
	if err != nil {
		t.Fatalf("get /web/: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("GET /web/ = %d, want 200", resp2.StatusCode)
	}
	if ct := resp2.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("GET /web/ content-type = %q, want text/html", ct)
	}
}
