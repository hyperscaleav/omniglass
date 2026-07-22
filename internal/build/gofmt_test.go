// Package build holds guards over the repository itself rather than over any
// one subsystem: properties that must hold across the whole tree, checked the
// same way everything else is checked.
package build

import (
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSourceIsGofmted fails when any Go file in the tree differs from gofmt.
//
// CI runs `go build` and `go test` and nothing else, so formatting drift had no
// gate at all: seven files were unformatted on main, and the way it surfaced was
// a directory-wide `gofmt -w` during an unrelated change sweeping up alignment
// fixes that then had to be reverted to keep that diff honest. An unformatted
// file is not a problem by itself; it becomes one when it turns every future
// editor-on-save into unrelated diff noise.
//
// This lives as a test rather than a CI step so `make test` catches it locally,
// before the commit, which is where the repository wants its gates.
func TestSourceIsGofmted(t *testing.T) {
	root := filepath.Join("..", "..")
	var unformatted []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case "node_modules", "dist", ".git", ".claude":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		formatted, err := format.Source(src)
		if err != nil {
			// A file that does not parse is the compiler's problem, not this
			// test's; reporting it here would only duplicate a build failure.
			return nil
		}
		if string(formatted) != string(src) {
			rel, _ := filepath.Rel(root, path)
			unformatted = append(unformatted, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(unformatted) > 0 {
		t.Errorf("%d file(s) are not gofmt'd; run `gofmt -w` on them:\n  %s",
			len(unformatted), strings.Join(unformatted, "\n  "))
	}
}
