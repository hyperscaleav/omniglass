package webui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

func TestMissingAssetIs404NotTheSPA(t *testing.T) {
	// #778: the fallback answered every miss with index.html and 200, so a
	// partial deploy looked healthy. A miss under a directory the build owns is
	// a genuine 404: nothing routes there but the build's own output.
	h := SPAHandler(fakeConsole())
	for _, p := range []string{
		"/assets/app-deadbeef.js",              // a chunk a partial deploy did not upload
		"/fonts/ibm-plex-sans-latin-700.woff2", // a face referenced but not built
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, rec.Code)
		}
		if contains(rec.Body.String(), `id="root"`) {
			t.Errorf("GET %s answered with index.html", p)
		}
	}
}

func TestAssetPrefixesComeFromTheBuild(t *testing.T) {
	// The prefixes are read off the build output, not a list kept by hand, so an
	// asset kind nobody enumerated is covered the day the build starts emitting
	// it. `media/` did not exist when #778 was fixed; no handler code names it.
	fsys := fakeConsole()
	fsys["media/intro-abc123.mp4"] = &fstest.MapFile{Data: []byte("fakemp4")}

	h := SPAHandler(fsys)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media/missing-000000.mp4", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /media/missing-000000.mp4 = %d, want 404", rec.Code)
	}

	// And the directory it lives in is served as before.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media/intro-abc123.mp4", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("GET /media/intro-abc123.mp4 = %d, want 200", rec.Code)
	}
}

func TestTheDirectoryHalfMatchesAWholeSegment(t *testing.T) {
	// The directory half of namesAFile, with no filesystem in the way. Only a
	// path *inside* one of the build's directories names a file by position; the
	// bare directory (answered before the rule, as a directory) and a look-alike
	// sharing its prefix do not.
	prefixes := []string{"assets", "fonts"}
	cases := map[string]bool{
		"assets/app-abc123": true,
		"fonts/any":         true,
		"assets":            false, // the bare directory, not a file in it
		"assetsomething/x":  false, // shares a prefix, is not the directory
		"locations/hq":      false, // a console route
	}
	for p, want := range cases {
		if got := namesAFile(p, prefixes); got != want {
			t.Errorf("namesAFile(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestDeepLinkStillRendersTheConsole(t *testing.T) {
	// The reason the catch-all exists. A console route at any depth must still
	// render the SPA. No route's last segment carries a dot (#808,
	// TestNoConsoleRouteCarriesADot), which is what lets one that does read as a
	// file.
	h := SPAHandler(fakeConsole())
	for _, p := range []string{"/locations/hq", "/nodes/edge-1/endpoints", "/products/samsung-qm55"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", p, rec.Code)
		}
		if !contains(rec.Body.String(), `id="root"`) {
			t.Errorf("GET %s did not render the console", p)
		}
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

func TestMissingRootLevelFileIs404(t *testing.T) {
	// #808, the half #778 left: a miss at the ROOT of the console filesystem
	// was still answered by the catch-all with index.html and 200, because the
	// build-directory rule only covers a path under assets/ or fonts/. The root
	// is exactly where public/ copies a contributor's new file (a manifest, a
	// touch icon, robots.txt), so the gap is positional, not about favicons.
	h := SPAHandler(fakeConsole())
	for _, p := range []string{"/favicn.svg", "/robots.txt", "/manifest.webmanifest", "/apple-touch-icon.png"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, rec.Code)
		}
		if contains(rec.Body.String(), `id="root"`) {
			t.Errorf("GET %s answered with the console shell", p)
		}
	}
	// The real root-level asset and the shell itself are untouched.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/favicon.svg", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != `<svg/>` {
		t.Errorf("GET /favicon.svg = %d %q, want 200 and the svg", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !contains(rec.Body.String(), `id="root"`) {
		t.Errorf("GET / = %d, want 200 and the shell", rec.Code)
	}
}

func TestABuildDirectoryIsNotAPage(t *testing.T) {
	// #808's note: /web/assets/ was assumed to render the shell. It did worse:
	// the file server answered a directory the build emitted with a listing of
	// every file in it. A build directory is not a page and not a file, so the
	// request is a 404 with or without the trailing slash.
	h := SPAHandler(fakeConsole())
	for _, p := range []string{"/assets/", "/assets", "/fonts/", "/fonts"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d (Location %q), want 404", p, rec.Code, rec.Header().Get("Location"))
		}
		if contains(rec.Body.String(), "app-abc123.js") || contains(rec.Body.String(), "woff2") {
			t.Errorf("GET %s listed the build directory: %q", p, rec.Body.String())
		}
	}
}

func TestDeepLinksStillRenderTheShell(t *testing.T) {
	// The catch-all exists so a deep link renders the SPA, and narrowing it must
	// not cost that. Every console route addresses by uuid or by name, and
	// neither can carry a dot (TestNoConsoleRouteCarriesADot pins the router
	// side of that premise).
	h := SPAHandler(fakeConsole())
	for _, p := range []string{
		"/locations",
		"/locations/east",
		"/components/0198f2c1-4a5b-7c3d-9e0f-1a2b3c4d5e6f",
		"/systems/hq-huddle-01?zoom=1",
		"/explore?node=hq.west.lobby",
		"/files/0198f2c1-4a5b-7c3d-9e0f-1a2b3c4d5e6f",
		"/definitely-not-a-route",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusOK || !contains(rec.Body.String(), `id="root"`) {
			t.Errorf("GET %s = %d, want 200 and the shell", p, rec.Code)
		}
	}
}

func TestNamesAFile(t *testing.T) {
	// The rule, pure: a miss names a file when its last segment carries an
	// extension, or when it sits under a directory the build emitted. Both halves
	// are derived (the platform's name rule; the build output), so neither goes
	// stale when the build starts emitting something nobody listed.
	dirs := []string{"assets", "fonts"}
	cases := map[string]bool{
		"favicn.svg":           true,
		"robots.txt":           true,
		"assets/app-abc123.js": true,
		"assets/app-abc123":    true, // no extension, but the build owns assets/
		"fonts/nested/face":    true,
		"img/hero.avif":        true, // a directory the build never emitted, but a file
		"":                     false,
		"locations":            false,
		"locations/east":       false,
		"systems/hq-huddle-01": false,
		"assetsomething/x":     false, // a whole first segment, not a prefix
		"etc/passwd":           false, // a cleaned traversal is a client route, and the shell
	}
	for p, want := range cases {
		if got := namesAFile(p, dirs); got != want {
			t.Errorf("namesAFile(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestUnbuiltConsoleStill404sAFile(t *testing.T) {
	// A binary built without -tags web serves the build-the-console placeholder
	// for a route, which is its point, but a request naming a file is a miss:
	// 200 with HTML there is the same false-healthy signal.
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

func TestNoConsoleRouteCarriesADot(t *testing.T) {
	// The premise the extension half rests on, checked against the router rather
	// than assumed: no console route pattern or smoke address carries a dot, and
	// every route parameter is :id (a uuid or a name, and the name rule,
	// internal/storage/name_rule.go, admits no dot), so a dotted last segment can
	// only be a file. A router change that adds a dotted route, or a parameter
	// whose values may carry one, fails here instead of silently turning its deep
	// links into 404s.
	src, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "lib", "routemanifest.ts"))
	if err != nil {
		t.Fatalf("read the route manifest: %v", err)
	}
	read, problems := routeManifestDots(string(src))
	if read < 40 {
		t.Fatalf("read %d path/smoke literals in the route manifest; its shape moved, so this guard no longer reads it", read)
	}
	for _, p := range problems {
		t.Error(p)
	}
}

func TestRouteManifestDotsReadsEveryLiteral(t *testing.T) {
	// The guard above is only as good as what it reads: a smoke address written
	// as a template literal, or a parameter other than :id, must not slip past.
	src := `export const UNKNOWN_UUID = "00000000-0000-4000-8000-000000000000";
const ADDR = "boi.17c.$sys.av";
  { path: "/ok", smoke: "/ok" },
  { path: "/ok/:id", smoke: ` + "`/ok/${UNKNOWN_UUID}`" + ` },
  { path: "/a/:address", smoke: ` + "`/a/${ADDR}`" + ` },
  { path: "/b/:id", smoke: ` + "`/b/${MISSING}`" + ` },
  { path: "/c.txt", smoke: "/c" },`
	read, problems := routeManifestDots(src)
	if read != 10 {
		t.Errorf("read %d literals, want all 10 (quoted and template)", read)
	}
	want := []string{`"/a/:address"`, `"/a/boi.17c.$sys.av"`, "MISSING", `"/c.txt"`}
	for _, w := range want {
		found := false
		for _, p := range problems {
			if strings.Contains(p, w) {
				found = true
			}
		}
		if !found {
			t.Errorf("no problem names %s; got %q", w, problems)
		}
	}
	if len(problems) != len(want) {
		t.Errorf("%d problems, want %d: %q", len(problems), len(want), problems)
	}
}

var (
	manifestConst   = regexp.MustCompile(`\bconst ([A-Z_][A-Z0-9_]*) = "([^"]*)"`)
	manifestLiteral = regexp.MustCompile("\\b(path|smoke): (?:\"([^\"]*)\"|`([^`]*)`)")
	manifestInterp  = regexp.MustCompile(`\$\{([^}]*)\}`)
	manifestParam   = regexp.MustCompile(`:([A-Za-z_][A-Za-z0-9_]*)`)
)

// routeManifestDots reads every path and smoke literal in the route manifest,
// double-quoted or a template literal (its ${NAME} resolved from the file's own
// string constants), and reports each one that carries a dot, each parameter
// other than :id, and each template naming a constant it cannot resolve. It
// returns how many literals it read, so a caller can tell a guard that stopped
// reading from one with nothing to report.
func routeManifestDots(src string) (int, []string) {
	consts := map[string]string{}
	for _, m := range manifestConst.FindAllStringSubmatch(src, -1) {
		consts[m[1]] = m[2]
	}
	var problems []string
	matches := manifestLiteral.FindAllStringSubmatch(src, -1)
	for _, m := range matches {
		kind, value := m[1], m[2]
		if m[3] != "" {
			value = manifestInterp.ReplaceAllStringFunc(m[3], func(ref string) string {
				name := manifestInterp.FindStringSubmatch(ref)[1]
				v, ok := consts[name]
				if !ok {
					problems = append(problems, fmt.Sprintf("route manifest %s %q interpolates %s, which is not a string constant this guard can resolve", kind, m[3], name))
				}
				return v
			})
		}
		if strings.Contains(value, ".") {
			problems = append(problems, fmt.Sprintf("route manifest %s %q carries a dot: the console server reads a dotted last segment as a file request (namesAFile)", kind, value))
		}
		if kind == "path" {
			for _, p := range manifestParam.FindAllStringSubmatch(value, -1) {
				if p[1] != "id" {
					problems = append(problems, fmt.Sprintf("route manifest path %q has parameter :%s; only :id (a uuid or a dot-free name) is known to be safe from namesAFile", value, p[1]))
				}
			}
		}
	}
	return len(matches), problems
}
