// Command migrationcheck fails when the migrations in a working tree would not
// apply in the order they were written (#1246): two files share a version, or
// a migration the tree adds over a base revision is not newer than the base's
// newest (a branch cut before another migration landed), or is stamped later
// than the clock. The working tree is read from disk, so a migration `make
// migration` just created counts before it is committed.
//
//	make migration-check                 # against origin/main (fetch it first)
//	make migration-check BASE=<rev>      # against any revision
//
// CI (.github/workflows/migrations.yml) runs it on a pull request against the
// base branch's tip, and on a push to main against the commit before the push.
// Exit: 0 clean, 1 a rule failed, 2 the base or the directory could not be read.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/db"
)

type options struct {
	Repo string    // the repository root; Dir is relative to it
	Base string    // the revision whose migrations the tree is judged against
	Dir  string    // the migrations directory, slash-separated
	Now  time.Time // the clock the not-in-the-future rule reads
}

func main() {
	base := flag.String("base", "origin/main", "the revision to judge the tree's added migrations against")
	dir := flag.String("dir", "db/migrations", "the migrations directory, relative to the repository root")
	flag.Parse()
	os.Exit(run(options{Repo: ".", Base: *base, Dir: *dir, Now: time.Now()}, os.Stdout, os.Stderr))
}

func run(o options, stdout, stderr io.Writer) int {
	head, err := treeNames(filepath.Join(o.Repo, filepath.FromSlash(o.Dir)))
	if err != nil {
		fmt.Fprintf(stderr, "migrationcheck: %v\n", err)
		return 2
	}
	base, err := baseNames(o.Repo, o.Base, o.Dir)
	if err != nil {
		fmt.Fprintf(stderr, "migrationcheck: %v\n", err)
		return 2
	}
	errs := append(db.CheckUnique(head), db.CheckAgainstBase(base, head, o.Now)...)
	for _, err := range errs {
		fmt.Fprintf(stderr, "migrationcheck: %v\n", err)
	}
	if len(errs) > 0 {
		return 1
	}
	fmt.Fprintf(stdout, "migrationcheck: %d migrations, %d added over %s (newest there: %s): ok\n",
		len(head), added(base, head), o.Base, newest(base))
	return 0
}

// treeNames lists the .sql files in the working tree's migrations directory,
// the set dbmate would embed.
func treeNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// baseNames lists the .sql files the base revision holds in dir, read from git
// so the base needs no checkout of its own.
func baseNames(repo, base, dir string) ([]string, error) {
	out, err := exec.Command("git", "-C", repo, "ls-tree", "--name-only", "--full-tree", base, "--", dir+"/").Output()
	if err != nil {
		msg := err.Error()
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			msg = strings.TrimSpace(string(ee.Stderr))
		}
		return nil, fmt.Errorf("read %s at %s (fetch it first, e.g. git fetch origin main): %s", dir, base, msg)
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasSuffix(line, ".sql") {
			names = append(names, path.Base(line))
		}
	}
	return names, nil
}

func added(base, head []string) int {
	in := map[string]bool{}
	for _, n := range base {
		in[n] = true
	}
	count := 0
	for _, n := range head {
		if !in[n] {
			count++
		}
	}
	return count
}

func newest(names []string) string {
	top := "none"
	for _, n := range names {
		if v, err := db.Version(n); err == nil && (top == "none" || v > top) {
			top = v
		}
	}
	return top
}
