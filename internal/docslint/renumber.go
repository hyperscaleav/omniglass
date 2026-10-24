package docslint

// RenumberADR is the one-command fix for two PRs that took the same next ADR
// number (#1095). The decisions lint names both files; the branch that lands
// second runs `make adr-renumber FROM=NNNN TO=MMMM`, which renames its decision
// file and rewrites every reference to the number across the repository: the
// ADR-NNNN mentions in docs, code comments, and skills, and the
// #adr-NNNN-<slug> anchors and adr-NNNN-<slug>.md file names. Run it on the
// branch before merging main, while ADR-NNNN means only the branch's decision.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// renumberSkipDirs are directories never rewritten: dependencies, build output,
// git's own store, and other worktrees nested under .claude/worktrees.
var renumberSkipDirs = map[string]bool{
	".git": true, "node_modules": true, "dist": true, ".astro": true, "vendor": true,
	"bin": true, "test-results": true, "worktrees": true, "public": true,
}

// renumberExts are the text files a reference can live in.
var renumberExts = map[string]bool{
	".md": true, ".mdx": true, ".go": true, ".ts": true, ".tsx": true, ".js": true, ".mjs": true,
	".cjs": true, ".astro": true, ".yaml": true, ".yml": true, ".json": true, ".sql": true,
	".css": true, ".txt": true, ".sh": true, ".toml": true, ".d2": true,
}

// RenumberADR moves decision `from` to number `to` under the repository root,
// returning the repo-relative paths it changed, sorted.
func RenumberADR(root string, from, to int) ([]string, error) {
	if from < 1 || from > 9999 || to < 1 || to > 9999 || from == to {
		return nil, fmt.Errorf("FROM and TO are two different numbers from 1 to 9999")
	}
	dir := filepath.Join(root, "docs", "src", "content", "decisions")
	holders := func(n int) ([]string, error) {
		matches, err := filepath.Glob(filepath.Join(dir, fmt.Sprintf("adr-%04d-*.md", n)))
		if err != nil {
			return nil, err
		}
		for i := range matches {
			matches[i] = filepath.Base(matches[i])
		}
		sort.Strings(matches)
		return matches, nil
	}
	src, err := holders(from)
	if err != nil {
		return nil, err
	}
	switch len(src) {
	case 0:
		return nil, fmt.Errorf("no decision file holds ADR-%04d under %s", from, dir)
	case 1:
	default:
		return nil, fmt.Errorf("ADR-%04d is held by %s, so its references cannot be told apart: renumber yours before merging main "+
			"(abort the merge, run this, then merge again)", from, strings.Join(src, " and "))
	}
	if taken, err := holders(to); err != nil {
		return nil, err
	} else if len(taken) > 0 {
		return nil, fmt.Errorf("ADR-%04d is taken by %s", to, strings.Join(taken, ", "))
	}

	oldName := src[0]
	newName := fmt.Sprintf("adr-%04d-%s", to, strings.TrimPrefix(oldName, fmt.Sprintf("adr-%04d-", from)))
	if err := os.Rename(filepath.Join(dir, oldName), filepath.Join(dir, newName)); err != nil {
		return nil, err
	}

	// ADR-0160, adr-0160-<slug> (an anchor or a file name): the prefix keeps its
	// case, and the word boundaries keep ADR-01601 and ADR-0016 out.
	ref := regexp.MustCompile(fmt.Sprintf(`(?i)\b(adr-)%04d\b`, from))
	repl := fmt.Sprintf("${1}%04d", to)
	var changed []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && renumberSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !renumberExts[filepath.Ext(path)] && d.Name() != "Makefile" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out := ref.ReplaceAll(b, []byte(repl))
		if string(out) == string(b) && filepath.Base(path) != newName {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		changed = append(changed, filepath.ToSlash(rel))
		if string(out) == string(b) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(path, out, info.Mode().Perm())
	})
	if err != nil {
		return changed, err
	}
	sort.Strings(changed)
	return changed, nil
}
