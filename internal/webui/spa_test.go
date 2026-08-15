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
		"index.html":                          {Data: []byte(`<!doctype html><div id="root"></div>`)},
		"assets/app-abc123.js":                {Data: []byte(`console.log("app")`)},
		"favicon.svg":                         {Data: []byte(`<svg/>`)},
		"fonts/ibm-plex-sans-latin-400.woff2": {Data: []byte("wOF2fake")},
	}
}

func TestServesFontsWithTheirOwnContentType(t *testing.T) {
	// The console serves its own typefaces (#775), so their content type is this
	// binary's job. net/http types by extension from the HOST's mime database,
	// and the runtime images this ships in (distroless, debian-slim) carry no
	// /etc/mime.types at all: without this, a woff2 is sniffed to
	// application/octet-stream, which any nosniff policy in front of the console
	// is entitled to refuse. The type must come from the binary, not the host.
	h := SPAHandler(fakeConsole())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/fonts/ibm-plex-sans-latin-400.woff2", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("font = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "font/woff2" {
		t.Errorf("font Content-Type = %q, want font/woff2", ct)
	}
}

func TestContentTypeComesFromTheBinaryNotTheHost(t *testing.T) {
	// The mapping itself, with no filesystem and no ambient mime database in the
	// way: on a developer box /etc/mime.types usually knows woff2 and the handler
	// test above would pass with no code at all, while the shipped image does not
	// know it. This is the assertion that cannot be satisfied by the host.
	cases := map[string]string{
		"fonts/ibm-plex-sans-latin-400.woff2":  "font/woff2",
		"fonts/jetbrains-mono-latin-700.woff2": "font/woff2",
		"assets/app-abc123.js":                 "", // net/http already types these
		"index.html":                           "",
		"favicon.svg":                          "",
	}
	for name, want := range cases {
		if got := contentTypeFor(name); got != want {
			t.Errorf("contentTypeFor(%q) = %q, want %q", name, got, want)
		}
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

func TestNoPathTraversal(t *testing.T) {
	// An absolute request path is cleaned at the root, and io/fs rejects any
	// remaining "..", so a traversal attempt cannot escape the embedded FS: it
	// resolves to a non-existent file and falls back to index.html, never a host
	// file. This documents the safety the http.FileServer + fs.Sub already give.
	h := SPAHandler(fakeConsole())
	for _, p := range []string{"/../../etc/passwd", "/assets/../../../../etc/passwd", "/..%2f..%2fetc/passwd"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200 (index fallback)", p, rec.Code)
		}
		if body := rec.Body.String(); !contains(body, `id="root"`) {
			t.Errorf("GET %s leaked a non-index body: %q", p, body)
		}
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
