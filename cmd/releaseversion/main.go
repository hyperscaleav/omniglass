// Command releaseversion prints the tag a release dispatch cuts on a release
// branch, read from the branch name and the repository's tags (#2139). The
// release workflow appends its output to $GITHUB_OUTPUT; `make release-plan`
// prints it as a preview.
//
//	go run ./cmd/releaseversion -branch release/0.1            # the next candidate
//	go run ./cmd/releaseversion -branch release/0.1 -final     # the final release
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/releaseversion"
)

func main() {
	branch := flag.String("branch", "", "the release branch, release/<major>.<minor>")
	final := flag.Bool("final", false, "release the newest candidate as final instead of cutting a new candidate")
	flag.Parse()

	tags, err := gitLines("tag", "--list", "v*")
	if err != nil {
		fail(err)
	}
	headTags, err := gitLines("tag", "--points-at", "HEAD")
	if err != nil {
		fail(err)
	}
	v, err := releaseversion.Next(releaseversion.Request{
		Branch:   *branch,
		Tags:     tags,
		HeadTags: headTags,
		Final:    *final,
	})
	if err != nil {
		fail(err)
	}
	fmt.Printf("tag=%s\nprerelease=%t\nprevious=%s\n", v.Tag, v.Prerelease, v.Previous)
}

func gitLines(args ...string) ([]string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.Fields(string(out)), nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "releaseversion:", err)
	os.Exit(1)
}
