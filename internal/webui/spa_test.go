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

func TestMissingAssetIs404(t *testing.T) {
	// #778: the SPA fallback used to answer EVERY miss with index.html and 200,
	// including requests that name a file. A missing JS chunk after a partial
	// deploy was served as HTML with 200, so the browser reported a syntax error
	// at line 1 instead of a missing file, a caching layer stored HTML under the
	// asset's URL, and any monitor keyed on 4xx saw a healthy origin while the
	// console was broken. That is the failure class this product exists to catch
	// in other people's estates.
	//
	// The kinds below deliberately include ones the console does not emit today
	// (.css, .wasm, .avif): the rule may not be a hand-kept extension list, or it
	// goes stale the first time the build emits something nobody enumerated.
	h := SPAHandler(fakeConsole())
	for _, p := range []string{
		"/assets/app-missing.js",
		"/assets/index-missing.css",
		"/assets/engine-missing.wasm",
		"/fonts/ibm-plex-sans-latin-900.woff2",
		"/img/hero-missing.avif",
		"/favicon.ico",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, rec.Code)
		}
		if body := rec.Body.String(); contains(body, `id="root"`) {
			t.Errorf("GET %s served the console shell as the miss: %q", p, body)
		}
	}
}

func TestMissingFileUnderABuildDirectoryIs404(t *testing.T) {
	// The extensionless miss under a directory the build owns. The directories
	// are read from the built filesystem at construction rather than hand-listed,
	// so an asset directory that did not exist when this was written is covered
	// by the same rule: media/ is not named anywhere in the handler.
	console := fakeConsole()
	console["media/clip-abc123.webm"] = &fstest.MapFile{Data: []byte("webm")}
	h := SPAHandler(console)
	for _, p := range []string{"/assets/app-missing", "/fonts/missing", "/media/missing"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, rec.Code)
		}
	}
	// And the directory's real file still serves.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media/clip-abc123.webm", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("GET /media/clip-abc123.webm = %d, want 200", rec.Code)
	}
}

func TestDeepLinkStillRendersTheShell(t *testing.T) {
	// The catch-all exists so a deep link into a console route renders the SPA
	// rather than 404ing. Narrowing it must not cost that: an entity name is
	// `^[a-z0-9][a-z0-9-]*$` (internal/storage/name.go) and a uuid carries no dot
	// either, so no client route ever names a file.
	h := SPAHandler(fakeConsole())
	for _, p := range []string{
		"/",
		"/locations",
		"/locations/east",
		"/components/0198f2c1-4a5b-7c3d-9e0f-1a2b3c4d5e6f",
		"/systems/hq-huddle-01?zoom=1",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200 (the shell)", p, rec.Code)
		}
		if body := rec.Body.String(); !contains(body, `id="root"`) {
			t.Errorf("GET %s did not serve the shell: %q", p, body)
		}
	}
}

func TestNamesAFile(t *testing.T) {
	// The rule itself, pure: a request names a file when its last segment carries
	// an extension, or when it sits under a directory the build emitted. Both
	// halves are derived (the platform's own name rule; the build output), so
	// neither goes stale against a build that emits something new.
	dirs := map[string]struct{}{"assets": {}, "fonts": {}}
	cases := map[string]bool{
		"assets/app-abc123.js": true,
		"fonts/plex.woff2":     true,
		"favicon.svg":          true,
		"assets/app-abc123":    true, // no extension, but the build owns assets/
		"fonts/nested/face":    true,
		"":                     false,
		"locations":            false,
		"locations/east":       false,
		"systems/hq-huddle-01": false,
		"etc/passwd":           false, // a cleaned traversal is still a client route
	}
	for p, want := range cases {
		if got := namesAFile(p, dirs); got != want {
			t.Errorf("namesAFile(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestUnbuiltConsoleStill404sAnAsset(t *testing.T) {
	// A binary built without `-tags web` serves the build-the-console placeholder
	// for a route, which is the point of it, but a request that names a file is
	// still a miss: answering 200 with HTML there hides an unbuilt console behind
	// the same false-healthy signal #778 is about.
	h := SPAHandler(fstest.MapFS{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app-abc123.js", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unbuilt asset = %d, want 404", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/locations", nil))
	if rec.Code != http.StatusOK || !contains(rec.Body.String(), "was not built") {
		t.Errorf("unbuilt route = %d %q, want 200 and the build hint", rec.Code, rec.Body.String())
	}
}
