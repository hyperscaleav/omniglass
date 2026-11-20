package build

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCODEOWNERSPathsExist fails when any explicit path in .github/CODEOWNERS
// does not exist in the repository tree.
//
// CODEOWNERS entries that become stale after a directory rename or move are
// silently dropped by GitHub: the ownership rule simply stops applying and no
// reviewer is added. The test fails early so the rename and the CODEOWNERS
// update travel together.
//
// Wildcard-only patterns (*, **, *.ext) have no single path to check and are
// skipped. Only patterns that name a concrete file or directory are verified.
func TestCODEOWNERSPathsExist(t *testing.T) {
	root := filepath.Join("..", "..")
	codeownersPath := filepath.Join(root, ".github", "CODEOWNERS")

	f, err := os.Open(codeownersPath)
	if err != nil {
		t.Fatalf("open CODEOWNERS: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// First field is the pattern; remainder are owners.
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		pattern := fields[0]

		// Skip pure-wildcard patterns: they match by content, not by path.
		if isWildcardOnly(pattern) {
			continue
		}

		// CODEOWNERS paths are repo-rooted with a leading slash; strip it.
		rel := strings.TrimPrefix(pattern, "/")
		// A trailing slash means a directory; strip it for the stat check.
		rel = strings.TrimSuffix(rel, "/")
		if rel == "" {
			continue
		}

		abs := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			t.Errorf("line %d: CODEOWNERS pattern %q points to %q which does not exist in the tree; update the path or remove the rule", lineNum, pattern, rel)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan CODEOWNERS: %v", err)
	}
}

func TestIsWildcardOnly(t *testing.T) {
	cases := []struct {
		pattern string
		want    bool
	}{
		{"*", true},
		{"**", true},
		{"*.md", true},
		{"/docs/src/content/**", true},
		{"/docs/*.md", true},
		{"/CLAUDE.md", false},
		{"/.github/", false},
		{"/internal/build/", false},
	}
	for _, c := range cases {
		t.Run(c.pattern, func(t *testing.T) {
			got := isWildcardOnly(c.pattern)
			if got != c.want {
				t.Errorf("isWildcardOnly(%q) = %v, want %v", c.pattern, got, c.want)
			}
		})
	}
}

// isWildcardOnly reports whether a CODEOWNERS pattern contains a glob
// character anywhere (*.go, /docs/**, /web/*.ts). Such patterns match by
// content rather than by a single path, so there is nothing to stat.
func isWildcardOnly(pattern string) bool {
	// Remove leading slash before checking.
	p := strings.TrimPrefix(pattern, "/")
	// If the pattern contains any glob character, it cannot be stat-checked.
	if strings.ContainsAny(p, "*?[") {
		return true
	}
	return false
}
