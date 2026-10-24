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

var adrNumberArg = regexp.MustCompile(`^(\d{1,9})(?:\.([2-9]|[1-9]\d+))?$`)

// parseADRNumber reads a FROM or TO argument (160, 0160, 1108, 1108.2) into the
// form a heading writes it in: zero-padded to four digits, a suffix from .2.
func parseADRNumber(arg string) (string, error) {
	m := adrNumberArg.FindStringSubmatch(arg)
	if m == nil || atoi(m[1]) == 0 {
		return "", fmt.Errorf("%q is not an ADR number: write it like 0160, 1108, or 1108.2 (a suffix from .2)", arg)
	}
	suffix := 1
	if m[2] != "" {
		suffix = atoi(m[2])
	}
	return adrKey(atoi(m[1]), suffix), nil
}

// RenumberADR moves decision `fromArg` to number `toArg` under the repository
// root, returning the repo-relative paths it changed, sorted. A number is
// written as a heading writes it (0160, 1108, 1108.2); a suffixed number's
// anchor drops the dot (#adr-NNNN2-<slug>), so mentions and file names take the
// dot form and anchors the dotless one.
func RenumberADR(root, fromArg, toArg string, opt RenumberOptions) ([]string, error) {
	from, err := parseADRNumber(fromArg)
	if err != nil {
		return nil, err
	}
	to, err := parseADRNumber(toArg)
	if err != nil {
		return nil, err
	}
	if from == to {
		return nil, fmt.Errorf("FROM and TO are the same number")
	}
	dir := filepath.Join(root, "docs", "src", "content", "decisions")
	holders := func(n string) ([]string, error) {
		matches, err := filepath.Glob(filepath.Join(dir, "adr-"+n+"-*.md"))
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
		return nil, fmt.Errorf("no decision file holds ADR-%s under %s", from, dir)
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
			return nil, fmt.Errorf("%s does not hold ADR-%s (held by %s)", opt.File, from, strings.Join(src, ", "))
		}
	case len(src) == 1:
		oldName = src[0]
	default:
		return nil, fmt.Errorf("ADR-%s is held by %s: name the one to move with FILE=<its file name>, "+
			"and only the lines your commits wrote are rewritten", from, strings.Join(src, " and "))
	}
	if taken, err := holders(to); err != nil {
		return nil, err
	} else if len(taken) > 0 {
		return nil, fmt.Errorf("ADR-%s is taken by %s", to, strings.Join(taken, ", "))
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

	newName := "adr-" + to + "-" + strings.TrimPrefix(oldName, "adr-"+from+"-")
	if err := os.Rename(filepath.Join(dir, oldName), filepath.Join(dir, newName)); err != nil {
		return nil, err
	}

	rewrite := adrRewriter(from, to)
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
		out := rewrite(b)
		if !isText(b) || (bytes.Equal(out, b) && path != movedPath) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if mine != nil && path != movedPath {
			if out, err = rewriteBlamed(root, rel, b, rewrite, mine); err != nil {
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
func rewriteBlamed(root, rel string, b []byte, rewrite func([]byte) []byte, mine map[string]bool) ([]byte, error) {
	out, err := exec.Command("git", "-C", root, "blame", "--line-porcelain", "--", rel).Output()
	if err != nil {
		if exec.Command("git", "-C", root, "ls-files", "--error-unmatch", "--", rel).Run() != nil {
			return rewrite(b), nil
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
			lines[i] = rewrite(line)
		}
	}
	return bytes.Join(lines, nil), nil
}

// adrRewriter rewrites one decision's references from number `from` to `to`
// (both as headings write them). Two forms:
//
//   - an anchor, #adr-<number without its dot>-, which is what a heading's slug
//     makes of ADR-NNNN.2 (#adr-NNNN2-<slug>);
//   - a mention or a file name, ADR-<number> or adr-<number>-<slug>.md, its
//     prefix keeping its case. The word boundaries keep a longer number out,
//     and a plain number followed by a suffix (ADR-NNNN in ADR-NNNN.2) is
//     another decision, left alone.
func adrRewriter(from, to string) func([]byte) []byte {
	anchor := regexp.MustCompile(`#adr-` + strings.ReplaceAll(from, ".", "") + `-`)
	anchorTo := []byte("#adr-" + strings.ReplaceAll(to, ".", "") + "-")
	mention := regexp.MustCompile(`(?i)\b(adr-)` + regexp.QuoteMeta(from) + `\b`)
	suffixAfter := regexp.MustCompile(`^\.\d`)
	return func(b []byte) []byte {
		b = anchor.ReplaceAll(b, anchorTo)
		var out []byte
		last := 0
		for _, m := range mention.FindAllSubmatchIndex(b, -1) {
			if suffixAfter.Match(b[m[1]:]) || (m[0] > 0 && b[m[0]-1] == '#') {
				continue
			}
			out = append(out, b[last:m[3]]...)
			out = append(out, to...)
			last = m[1]
		}
		if out == nil {
			return b
		}
		return append(out, b[last:]...)
	}
}
