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

// isWildcardOnly reports whether a CODEOWNERS pattern is a pure wildcard
// (contains no path separator and is or contains only glob characters).
// Such patterns match by content rather than by a single directory path
// and cannot be verified with a stat.
func isWildcardOnly(pattern string) bool {
	// Remove leading slash before checking.
	p := strings.TrimPrefix(pattern, "/")
	// If there is no slash left and the pattern is entirely glob chars, skip.
	if !strings.Contains(p, "/") && strings.ContainsAny(p, "*?[") {
		return true
	}
	return false
}
