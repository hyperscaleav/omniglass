package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

// These prove the SPA-fallback routing against a fake filesystem (no Vite build
// needed). The real embedded bytes are exercised by spa_embed_test.go under
// `-tags web`.

func fakeConsole() fstest.MapFS {
	return fstest.MapFS{
		"index.html":           {Data: []byte(`<!doctype html><div id="root"></div>`)},
		"assets/app-abc123.js": {Data: []byte(`console.log("app")`)},
		"favicon.svg":          {Data: []byte(`<svg/>`)},
	}
}

func TestServesRealFile(t *testing.T) {
	h := SPAHandler(fakeConsole())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app-abc123.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("asset = %d, want 200", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("asset Cache-Control = %q, want immutable", cc)
	}
}

func TestFallsBackToIndexForClientRoute(t *testing.T) {
	h := SPAHandler(fakeConsole())
	// A client-side route (/web stripped to /locations/hq) has no file; the SPA
	// handler must serve index.html so the Solid router resolves it.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/locations/hq", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("client route = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); body == "" || body[:9] != "<!doctype" {
		t.Errorf("client route body = %q, want index.html", body)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("index Cache-Control = %q, want no-cache", cc)
	}
}

func TestPlaceholderWhenUnbuilt(t *testing.T) {
	// No index.html: the unbuilt-console placeholder, not a 404.
	h := SPAHandler(fstest.MapFS{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("placeholder = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); !contains(body, "was not built") {
		t.Errorf("placeholder body = %q, want the build hint", body)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
