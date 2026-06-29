//go:build web

package webui

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These run only under `-tags web`, against the real go:embed'd Vite build, so
// they prove the build -> embed -> serve chain end to end (the untagged tests
// prove the routing against a fake FS).

func TestEmbeddedConsoleServesBuiltShell(t *testing.T) {
	srv := httptest.NewServer(SPA())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("get /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `id="root"`) {
		t.Fatalf("GET / served the placeholder, not the built shell: %q", body)
	}
}

func TestEmbeddedConsoleServesHashedAsset(t *testing.T) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		t.Fatal(err)
	}
	var asset string
	_ = fs.WalkDir(sub, "assets", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && asset == "" {
			asset = p
		}
		return nil
	})
	if asset == "" {
		t.Fatal("no asset under dist/assets; Vite produced none")
	}
	rec := httptest.NewRecorder()
	SPA().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+asset, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /%s = %d, want 200", asset, rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("asset Cache-Control = %q, want immutable", cc)
	}
}
