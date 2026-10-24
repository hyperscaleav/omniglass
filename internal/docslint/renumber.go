package docslint

// RenumberADR is the one-command fix for two PRs that took the same next ADR
// number (#1095). The decisions lint names both files, and the branch that
// lands second moves its decision with `make adr-renumber FROM=NNNN TO=MMMM`,
// which renames the decision file and rewrites every reference to the number
// across the repository: the ADR-NNNN mentions in docs, code comments, and
// skills, and the #adr-NNNN-<slug> anchors and adr-NNNN-<slug>.md file names.
//
// Before main is merged in, the number means only the branch's decision and
// every reference is rewritten. Once both decisions are in the tree (after the
// merge, or on main when both PRs landed), FILE names the decision to move and
// only the lines written by the commits after SINCE (default origin/main, or
// not yet committed) are rewritten: git blame tells the two decisions'
// references apart.

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// RenumberOptions scope a renumber to one of two decisions holding a number.
type RenumberOptions struct {
	// File is the decision file to move (adr-NNNN-<slug>.md). Required when two
	// files hold the number.
	File string
	// Since bounds the commits whose lines are rewritten when File is set: only
	// lines blamed to a commit in Since..HEAD, or not yet committed. Defaults to
	// origin/main.
	Since string
}

// renumberSkipDirs are directories never rewritten: dependencies, build output,
// git's own store, published assets, and other worktrees nested under
// .claude/worktrees.
var renumberSkipDirs = map[string]bool{
	".git": true, "node_modules": true, "dist": true, ".astro": true, "vendor": true,
	"bin": true, "test-results": true, "worktrees": true, "public": true,
}

// isText reports whether b is a file a reference can live in: valid UTF-8 with
// no NUL byte, whatever its extension (a .proto comment cites ADRs as surely as
// a .go one does).
func isText(b []byte) bool {
	head := b
	if len(head) > 8000 {
		head = head[:8000]
	}
	return bytes.IndexByte(head, 0) == -1 && utf8.Valid(b)
}

// RenumberADR moves decision `from` to number `to` under the repository root,
// returning the repo-relative paths it changed, sorted.
func RenumberADR(root string, from, to int, opt RenumberOptions) ([]string, error) {
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
	if len(src) == 0 {
		return nil, fmt.Errorf("no decision file holds ADR-%04d under %s", from, dir)
	}
	oldName := ""
	switch {
	case opt.File != "":
		for _, n := range src {
			if n == filepath.Base(opt.File) {
				oldName = n
			}
		}
		if oldName == "" {
			return nil, fmt.Errorf("%s does not hold ADR-%04d (held by %s)", opt.File, from, strings.Join(src, ", "))
		}
	case len(src) == 1:
		oldName = src[0]
	default:
		return nil, fmt.Errorf("ADR-%04d is held by %s: name the one to move with FILE=<its file name>, "+
			"and only the lines your commits wrote are rewritten", from, strings.Join(src, " and "))
	}
	if taken, err := holders(to); err != nil {
		return nil, err
	} else if len(taken) > 0 {
		return nil, fmt.Errorf("ADR-%04d is taken by %s", to, strings.Join(taken, ", "))
	}

	// Scoped mode: the commits whose lines are this decision's references.
	var mine map[string]bool
	if opt.File != "" {
		since := opt.Since
		if since == "" {
			since = "origin/main"
		}
		rel := filepath.ToSlash(filepath.Join("docs", "src", "content", "decisions", oldName))
		if err := exec.Command("git", "-C", root, "cat-file", "-e", since+":"+rel).Run(); err == nil {
			return nil, fmt.Errorf("%s already existed at %s, so it is the decision that landed first: move the other one", oldName, since)
		}
		out, err := exec.Command("git", "-C", root, "rev-list", since+"..HEAD").Output()
		if err != nil {
			return nil, fmt.Errorf("git rev-list %s..HEAD: %v", since, err)
		}
		mine = map[string]bool{strings.Repeat("0", 40): true} // not yet committed
		for _, sha := range strings.Fields(string(out)) {
			mine[sha] = true
		}
	}

	newName := fmt.Sprintf("adr-%04d-%s", to, strings.TrimPrefix(oldName, fmt.Sprintf("adr-%04d-", from)))
	if err := os.Rename(filepath.Join(dir, oldName), filepath.Join(dir, newName)); err != nil {
		return nil, err
	}

	// ADR-NNNN and adr-NNNN-<slug> (an anchor or a file name): the prefix keeps
	// its case, and the word boundaries keep a five-digit run or a shorter
	// number out.
	ref := regexp.MustCompile(fmt.Sprintf(`(?i)\b(adr-)%04d\b`, from))
	repl := []byte(fmt.Sprintf("${1}%04d", to))
	movedPath := filepath.Join(dir, newName)
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
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !isText(b) || (!ref.Match(b) && path != movedPath) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out := ref.ReplaceAll(b, repl)
		if mine != nil && path != movedPath {
			if out, err = rewriteBlamed(root, rel, b, ref, repl, mine); err != nil {
				return err
			}
		}
		if !bytes.Equal(out, b) || path == movedPath {
			changed = append(changed, filepath.ToSlash(rel))
		}
		if bytes.Equal(out, b) {
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

// rewriteBlamed rewrites the references on the lines of rel that git blame
// attributes to one of the `mine` commits. A file git does not track yet is
// wholly the branch's, so every reference in it is rewritten.
func rewriteBlamed(root, rel string, b []byte, ref *regexp.Regexp, repl []byte, mine map[string]bool) ([]byte, error) {
	out, err := exec.Command("git", "-C", root, "blame", "--line-porcelain", "--", rel).Output()
	if err != nil {
		if exec.Command("git", "-C", root, "ls-files", "--error-unmatch", "--", rel).Run() != nil {
			return ref.ReplaceAll(b, repl), nil
		}
		return nil, fmt.Errorf("git blame %s: %v", rel, err)
	}
	owner := map[int]string{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && len(f[0]) == 40 && strings.Trim(f[0], "0123456789abcdef") == "" {
			if n, err := strconv.Atoi(f[2]); err == nil {
				owner[n] = f[0]
			}
		}
	}
	lines := bytes.SplitAfter(b, []byte("\n"))
	for i, line := range lines {
		if mine[owner[i+1]] {
			lines[i] = ref.ReplaceAll(line, repl)
		}
	}
	return bytes.Join(lines, nil), nil
}
