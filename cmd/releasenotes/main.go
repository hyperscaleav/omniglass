// Command releasenotes prints a release's notes, grouped by change type, from
// the commits between the previous final release and the commit being tagged
// (#2142). The release workflow passes the notes to `gh release create` and,
// on a final release, comments on the PRs it lists.
//
//	go run ./cmd/releasenotes -repo hyperscaleav/omniglass -tag v0.1.0-rc.1 -previous v0.0.0
//	go run ./cmd/releasenotes ... -prs-file prs.txt    # also write the PR numbers, one a line
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/releasenotes"
)

func main() {
	repo := flag.String("repo", "", "owner/name, for links")
	tag := flag.String("tag", "", "the tag being released")
	previous := flag.String("previous", "", "the previous final release; empty for the whole history")
	to := flag.String("to", "HEAD", "the commit being released")
	prsFile := flag.String("prs-file", "", "write the PR numbers here, one a line")
	flag.Parse()
	if *repo == "" || *tag == "" {
		fail(fmt.Errorf("-repo and -tag are required"))
	}

	rng := *to
	if *previous != "" {
		rng = *previous + ".." + *to
	}
	// Unit and record separators keep subjects and bodies apart whatever they hold.
	out, err := exec.Command("git", "log", "--format=%s%x1f%b%x1e", rng).Output()
	if err != nil {
		fail(fmt.Errorf("git log %s: %w", rng, err))
	}
	var commits []releasenotes.Commit
	for _, rec := range strings.Split(string(out), "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		subject, body, _ := strings.Cut(rec, "\x1f")
		commits = append(commits, releasenotes.Commit{Subject: subject, Body: body})
	}

	notes := releasenotes.Render(releasenotes.Input{
		Repo: *repo, Tag: *tag, Previous: *previous, Commits: commits,
	})
	fmt.Print(notes.Markdown)

	if *prsFile != "" {
		var b strings.Builder
		for _, n := range notes.PRs {
			b.WriteString(strconv.Itoa(n) + "\n")
		}
		if err := os.WriteFile(*prsFile, []byte(b.String()), 0o644); err != nil {
			fail(err)
		}
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "releasenotes:", err)
	os.Exit(1)
}
