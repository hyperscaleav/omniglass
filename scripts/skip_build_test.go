package scripts

import (
	"os/exec"
	"strings"
	"testing"
)

// SKIP_BUILD=1 lets a CI job that loaded the shared build (#2597) run the lab
// targets without a second docker build; the default still builds.
func TestMakeAllSkipBuild(t *testing.T) {
	dry := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("make", append([]string{"-n", "-C", ".."}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("make %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	if out := dry("all"); !strings.Contains(out, "docker build") {
		t.Errorf("default make must build the image:\n%s", out)
	}
	out := dry("all", "SKIP_BUILD=1")
	if strings.Contains(out, "docker build") {
		t.Errorf("SKIP_BUILD=1 must not build:\n%s", out)
	}
	if !strings.Contains(out, "docker image inspect omniglass:dev") {
		t.Errorf("SKIP_BUILD=1 must check the prebuilt image is loaded:\n%s", out)
	}
}
