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

func TestEmbeddedConsoleCarriesItsTypefaces(t *testing.T) {
	// The console renders in typefaces this binary serves, never in typefaces it
	// fetches from a third party (#775): an estate on a closed campus or AV
	// network is the deployment this product targets, and a capture that lost the
	// race to a font CDN is what turned main red. The chain that has to hold is
	// vendored file -> Vite output -> go:embed -> served, so assert it here rather
	// than trusting the build.
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := fs.ReadDir(sub, "fonts")
	if err != nil {
		t.Fatalf("no fonts/ in the embedded console: %v", err)
	}
	var woff2 []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".woff2") {
			woff2 = append(woff2, e.Name())
		}
	}
	if len(woff2) == 0 {
		t.Fatal("fonts/ carries no woff2; the console would render in fallback metrics")
	}
	// OFL 1.1 permits the redistribution and requires the license to travel with
	// the fonts, so the license text ships inside the binary too.
	for _, lic := range []string{"fonts/ibm-plex-sans-OFL.txt", "fonts/jetbrains-mono-OFL.txt"} {
		if _, err := fs.Stat(sub, lic); err != nil {
			t.Errorf("embedded console is missing %s: %v", lic, err)
		}
	}

	rec := httptest.NewRecorder()
	SPA().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/fonts/"+woff2[0], nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /fonts/%s = %d, want 200", woff2[0], rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "font/woff2" {
		t.Errorf("font Content-Type = %q, want font/woff2", ct)
	}

	// And the shell asks for them locally: a remote <link> here is the bug itself.
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(index), "fonts.googleapis.com") || strings.Contains(string(index), "fonts.gstatic.com") {
		t.Error("index.html still links a font CDN")
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

func TestEmbeddedConsole404sAMissingAsset(t *testing.T) {
	// #778 against the REAL build output, which is the assertion that matters:
	// the prefixes are derived from whatever Vite and public/ actually emitted,
	// so this fails if the build stops emitting a directory the fake-FS tests
	// assume, or starts emitting one the derivation cannot see.
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := fs.ReadDir(sub, ".")
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) == 0 {
		t.Fatal("the embedded console has no asset directories; the build did not run")
	}

	h := SPA()
	for _, dir := range dirs {
		// A name no content hash can collide with, under a directory the build
		// really owns: a broken deploy, and a broken deploy must not read as 200.
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+dir+"/omniglass-no-such-file-000000.bin", nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET /%s/<missing> = %d, want 404", dir, rec.Code)
		}
		if strings.Contains(rec.Body.String(), `id="root"`) {
			t.Errorf("GET /%s/<missing> answered with the console shell", dir)
		}
	}

	// And the catch-all still does its job for a real console route.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/locations/hq", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `id="root"`) {
		t.Errorf("GET /locations/hq = %d, want 200 with the console shell", rec.Code)
	}
}
