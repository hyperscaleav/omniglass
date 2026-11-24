package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These drive scripts/go-version.sh and scripts/check-go-pin.sh (#2596): go.mod
// is the one place the Go version is written, and the image build reads it.

func runScript(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out, err := exec.Command("bash", args...).CombinedOutput()
	return string(out), err
}

func TestGoVersionPrefersToolchain(t *testing.T) {
	dir := t.TempDir()
	mod := filepath.Join(dir, "go.mod")
	os.WriteFile(mod, []byte("module x\n\ngo 1.26.0\n"), 0o644)
	if out, err := runScript(t, "go-version.sh", mod); err != nil || strings.TrimSpace(out) != "1.26.0" {
		t.Fatalf("go directive: %q %v", out, err)
	}
	os.WriteFile(mod, []byte("module x\n\ngo 1.26.0\n\ntoolchain go1.26.10\n"), 0o644)
	if out, err := runScript(t, "go-version.sh", mod); err != nil || strings.TrimSpace(out) != "1.26.10" {
		t.Fatalf("toolchain directive: %q %v", out, err)
	}
}

func pinRepo(t *testing.T, dockerfile, makefile, workflow string) string {
	t.Helper()
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, ".github/workflows"), 0o755)
	os.WriteFile(filepath.Join(d, "Dockerfile"), []byte(dockerfile), 0o644)
	os.WriteFile(filepath.Join(d, "Makefile"), []byte(makefile), 0o644)
	os.WriteFile(filepath.Join(d, ".github/workflows/i.yml"), []byte(workflow), 0o644)
	return d
}

const goodDocker = "ARG GO_VERSION\nFROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS build\n"
const goodMake = "all:\n\tdocker build -t a --build-arg GO_VERSION=$(GO_VERSION) .\n\tdocker build --target x \\\n\t\t--build-arg GO_VERSION=$(GO_VERSION) --output o .\n\tdocker build -q -t other internal/x\n"
const goodWF = "steps:\n  - name: b\n    uses: docker/build-push-action@abc\n    with:\n      build-args: |\n        GO_VERSION=${{ x }}\n"

func TestCheckGoPin(t *testing.T) {
	cases := []struct {
		name, df, mk, wf string
		wantFail         bool
	}{
		{"clean", goodDocker, goodMake, goodWF, false},
		{"hard-coded image", "ARG GO_VERSION\nFROM golang:1.26.0-bookworm AS build\n", goodMake, goodWF, true},
		{"arg with default", "ARG GO_VERSION=1.26\nFROM golang:${GO_VERSION}-bookworm\n", goodMake, goodWF, true},
		{"make build lacks arg", goodDocker, "all:\n\tdocker build -t a .\n", goodWF, true},
		{"make continuation lacks arg", goodDocker, "all:\n\tdocker build --target x \\\n\t\t--output o .\n", goodWF, true},
		{"workflow lacks arg", goodDocker, goodMake, "steps:\n  - uses: docker/build-push-action@abc\n    with:\n      context: .\n", true},
	}
	for _, c := range cases {
		root := pinRepo(t, c.df, c.mk, c.wf)
		out, err := runScript(t, "check-go-pin.sh", "--workflows", root)
		if (err != nil) != c.wantFail {
			t.Errorf("%s: fail=%v want %v\n%s", c.name, err != nil, c.wantFail, out)
		}
	}
}
