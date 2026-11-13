package build

import (
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// imageHomes are the only places a committed image may live, each with why it
// exists. A prefix ends in "/" so a sibling directory sharing a name stem
// (docs/public/screenshots-old/) is not swept in.
var imageHomes = []struct{ prefix, why string }{
	{"docs/public/screenshots/", "clean docs renders the screenshot directive embeds"},
	{"docs/screenshots/baseline/", "the masked twins of the clean docs renders, the diff baseline"},
	{"docs/public/walkthroughs/", "walkthrough stills copied by `make docs-walkthroughs`"},
	{"docs/public/d2/", "D2 diagram renders (SVG, not a raster, but an image home all the same)"},
	{".github/screenshots/", "PR evidence, one directory per PR, including walkthroughs/ subtrees"},
	{"internal/avatar/testdata/", "a test fixture the avatar renderer reads"},
}

var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true}

// strayImages returns the files of tracked that are raster images outside every
// entry of imageHomes, in the order given.
func strayImages(tracked []string) []string {
	var stray []string
	for _, f := range tracked {
		if !imageExts[strings.ToLower(filepath.Ext(f))] {
			continue
		}
		home := false
		for _, h := range imageHomes {
			if strings.HasPrefix(f, h.prefix) {
				home = true
				break
			}
		}
		if !home {
			stray = append(stray, f)
		}
	}
	return stray
}

func TestStrayImagesAreFoundOutsideTheHomes(t *testing.T) {
	tracked := []string{
		".prshots/components.png",
		"test.png",
		"web/src/logo.JPG",
		"docs/public/screenshots-old/a.png",
		"docs/public/screenshots/secrets.png",
		"docs/screenshots/baseline/secrets.png",
		"docs/public/walkthroughs/set-up-monitoring/01.png",
		"docs/public/d2/flow.svg",
		".github/screenshots/922/walkthroughs/x/01.webp",
		"internal/avatar/testdata/golden.png",
		"README.md",
	}
	want := []string{
		".prshots/components.png",
		"test.png",
		"web/src/logo.JPG",
		"docs/public/screenshots-old/a.png",
	}
	if got := strayImages(tracked); !reflect.DeepEqual(got, want) {
		t.Fatalf("strayImages = %v, want %v", got, want)
	}
}

// TestNoImageIsCommittedOutsideItsHome refuses a committed image anywhere but
// a home in imageHomes, so a stray capture directory (.prshots/, #922) cannot
// land on main again. It reads `git ls-files`, so it sees exactly what is
// committed, not what a local run left in the tree.
func TestNoImageIsCommittedOutsideItsHome(t *testing.T) {
	out, err := exec.Command("git", "-C", filepath.Join("..", ".."), "ls-files").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	if stray := strayImages(strings.Split(string(out), "\n")); len(stray) > 0 {
		var homes []string
		for _, h := range imageHomes {
			homes = append(homes, h.prefix)
		}
		t.Fatalf("images committed outside their homes (%s); move them to a home or drop them:\n  %s",
			strings.Join(homes, ", "), strings.Join(stray, "\n  "))
	}
}
