package profile

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestDeviceWebTemplates_NoBareAbsoluteAPICalls is a regression
// test for issue #51: device admin pages served inside the
// console iframe must not call fetch() with a bare leading-slash
// path. The iframe loads under /proxy/sites/<site>/devices/<id>/
// web/<port>/ on the console origin, so a bare "/api/foo" resolves
// against the console (which 404s) instead of routing back through
// the proxy passthrough to the device.
//
// The fix: each template has an apiURL() helper that prepends
// location.pathname, and send() / fetch() call sites pass paths
// through it. This test enforces the convention so a future edit
// can't quietly reintroduce the bug.
//
// Two assertions per template:
//
//  1. The template references apiURL(...) at least once. Catches
//     "the helper was deleted" regressions.
//  2. The template contains no bare fetch('/api/... or
//     fetch("/api/... call. Catches "added a new control without
//     wrapping it" regressions.
func TestDeviceWebTemplates_NoBareAbsoluteAPICalls(t *testing.T) {
	root := webTemplatesRoot(t)

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read web templates dir: %v", err)
	}

	// Match fetch('/api/... and fetch("/api/..., leading slash is
	// the bug signature. Wrapping helpers like apiURL('/api/...')
	// are fine because their first character is "(", not the quote.
	bareAbsolute := regexp.MustCompile(`fetch\(\s*['"]/api/`)

	var found int
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		path := filepath.Join(root, ent.Name(), "index.gotempl")
		body, err := os.ReadFile(path)
		if err != nil {
			// Some web template dirs ship without an index page (e.g.
			// per-route fragments only); skip silently.
			if os.IsNotExist(err) {
				continue
			}
			t.Errorf("read %s: %v", path, err)
			continue
		}
		found++
		text := string(body)

		if !strings.Contains(text, "apiURL(") {
			t.Errorf("%s: missing apiURL(...) helper. Every device-page template "+
				"must wrap fetch paths with apiURL so absolute /api/... paths "+
				"route back through the iframe's proxy prefix. See issue #51.",
				path)
		}

		if loc := bareAbsolute.FindStringIndex(text); loc != nil {
			line := strings.Count(text[:loc[0]], "\n") + 1
			t.Errorf("%s:%d: bare fetch('/api/...') call would 404 in the "+
				"console iframe, wrap with apiURL(). See issue #51.",
				path, line)
		}
	}

	// Cross-check that we actually scanned something, a future repo
	// reorg that moves the templates would otherwise silently pass.
	if found == 0 {
		t.Fatalf("no index.gotempl files found under %s; templates may have moved", root)
	}
}

func webTemplatesRoot(t *testing.T) string {
	t.Helper()
	root := findProfilesRoot(t)
	return filepath.Join(root, "templates", "web")
}
