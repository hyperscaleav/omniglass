package scripts

import (
	"os"
	"path/filepath"
	"testing"
)

// These drive scripts/check-node-pin.sh (#2170): .nvmrc is the one place the Node
// major is written. Workflows read it through setup-node's node-version-file, the
// session hook reads it, and the Dockerfile's node image must name the same major.

func nodePinRepo(t *testing.T, nvmrc, dockerfile, workflow, hook string) string {
	t.Helper()
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, ".github/workflows"), 0o755)
	os.MkdirAll(filepath.Join(d, ".claude/hooks"), 0o755)
	if nvmrc != "" {
		os.WriteFile(filepath.Join(d, ".nvmrc"), []byte(nvmrc), 0o644)
	}
	os.WriteFile(filepath.Join(d, "Dockerfile"), []byte(dockerfile), 0o644)
	os.WriteFile(filepath.Join(d, ".github/workflows/i.yml"), []byte(workflow), 0o644)
	os.WriteFile(filepath.Join(d, ".claude/hooks/session-env-check.sh"), []byte(hook), 0o644)
	return d
}

const goodNvmrc = "26\n"
const goodNodeDocker = "FROM --platform=$BUILDPLATFORM node:26-bookworm-slim AS web\nARG GO_VERSION\n"
const goodNodeWF = "steps:\n  - uses: actions/setup-node@abc # v7\n    with:\n      node-version-file: .nvmrc\n      cache: npm\n"
const goodHook = "NODE_MAJOR_PIN=$(tr -d '[:space:]' < \"$root/.nvmrc\")\n"

func TestCheckNodePin(t *testing.T) {
	cases := []struct {
		name, nvmrc, df, wf, hook string
		wantFail                  bool
	}{
		{"clean", goodNvmrc, goodNodeDocker, goodNodeWF, goodHook, false},
		{"no nvmrc", "", goodNodeDocker, goodNodeWF, goodHook, true},
		{"nvmrc not a bare major", "26.13.1\n", goodNodeDocker, goodNodeWF, goodHook, true},
		{"dockerfile on another major", goodNvmrc, "FROM node:22-bookworm-slim AS web\n", goodNodeWF, goodHook, true},
		{"dockerfile has no node image", goodNvmrc, "FROM golang:1.26 AS build\n", goodNodeWF, goodHook, true},
		{"workflow restates the version", goodNvmrc, goodNodeDocker,
			"steps:\n  - uses: actions/setup-node@abc\n    with:\n      node-version: \"26\"\n", goodHook, true},
		{"setup-node reads no file", goodNvmrc, goodNodeDocker,
			"steps:\n  - uses: actions/setup-node@abc\n    with:\n      cache: npm\n  - run: echo\n", goodHook, true},
		{"hook hard-codes the major", goodNvmrc, goodNodeDocker, goodNodeWF, "NODE_MAJOR_PIN=\"26\"\n", true},
	}
	for _, c := range cases {
		root := nodePinRepo(t, c.nvmrc, c.df, c.wf, c.hook)
		out, err := runScript(t, "check-node-pin.sh", root)
		if (err != nil) != c.wantFail {
			t.Errorf("%s: fail=%v want %v\n%s", c.name, err != nil, c.wantFail, out)
		}
	}
}

// The repository itself passes: every pin agrees with .nvmrc.
func TestRepoNodePin(t *testing.T) {
	if out, err := runScript(t, "check-node-pin.sh", ".."); err != nil {
		t.Fatalf("the repo's Node pins disagree:\n%s", out)
	}
}
