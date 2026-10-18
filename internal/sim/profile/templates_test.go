package profile

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestDeviceWebTemplates_NoBareAbsoluteAPICalls guards the device
// admin pages' apiURL() convention (originally omniglass-lab#51). Each
// page is served by the simulated device's own HTTP listener, and its
// apiURL() helper prepends location.pathname so absolute "/api/..."
// paths stay relative to wherever the page is served instead of
// resolving against the bare origin.
//
// send() / fetch() call sites pass paths through apiURL(). This test
// enforces the convention so a future edit can't quietly bypass it.
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

	// Match fetch('/api/... and fetch("/api/..., a leading slash
	// means the call skipped apiURL(). Wrapping helpers like apiURL('/api/...')
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
				"stay relative to wherever the page is served.",
				path)
		}

		if loc := bareAbsolute.FindStringIndex(text); loc != nil {
			line := strings.Count(text[:loc[0]], "\n") + 1
			t.Errorf("%s:%d: bare fetch('/api/...') call bypasses the page's "+
				"served path, wrap with apiURL().",
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
