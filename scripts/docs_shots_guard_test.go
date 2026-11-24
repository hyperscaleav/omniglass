package scripts

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// These drive scripts/docs-shots-guard.sh (#2561): a PR may add a docs shot but
// may not modify, delete or rename an existing one, and the recapture branch
// is the one exemption.

func docsShotsGuard(t *testing.T, headRef, diff string, repos ...string) (string, int) {
	t.Helper()
	requireTools(t)
	cmd := exec.Command("bash", append([]string{"docs-shots-guard.sh", headRef}, repos...)...)
	cmd.Stdin = strings.NewReader(diff)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return errb.String(), code
}

func TestDocsShotsGuardAllowsANewShotAndOtherFiles(t *testing.T) {
	diff := "A\tdocs/public/screenshots/new-id.png\nA\tdocs/screenshots/baseline/new-id.png\nM\tweb/src/App.tsx\nM\tdocs/src/content/docs/x.md\n"
	if out, code := docsShotsGuard(t, "claude/feat-x", diff); code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
}

func TestDocsShotsGuardRefusesModifyDeleteAndRename(t *testing.T) {
	for _, diff := range []string{
		"M\tdocs/public/screenshots/fleet.png\n",
		"M\tdocs/screenshots/baseline/fleet.png\n",
		"D\tdocs/public/screenshots/fleet.png\n",
		"R100\tdocs/public/screenshots/fleet.png\tdocs/public/screenshots/fleet2.png\n",
	} {
		out, code := docsShotsGuard(t, "claude/feat-x", diff)
		if code == 0 || !strings.Contains(out, "Main recaptures") {
			t.Errorf("diff %q: exit %d, output %q, want a refusal saying main recaptures", diff, code, out)
		}
	}
}

func TestDocsShotsGuardExemptsTheRecaptureBranch(t *testing.T) {
	diff := "M\tdocs/public/screenshots/fleet.png\nM\tdocs/screenshots/baseline/fleet.png\n"
	for _, ref := range []string{"claude/docs-shots-recapture", "claude/docs-shots-recapture-release-1-4"} {
		if out, code := docsShotsGuard(t, ref, diff, "o/r", "o/r"); code != 0 {
			t.Errorf("%s: exit %d: %s", ref, code, out)
		}
	}
}

func TestDocsShotsGuardDoesNotExemptAFork(t *testing.T) {
	diff := "M\tdocs/public/screenshots/fleet.png\n"
	if _, code := docsShotsGuard(t, "claude/docs-shots-recapture", diff, "fork/r", "o/r"); code == 0 {
		t.Fatal("a fork's branch of the recapture name skipped the guard")
	}
	if _, code := docsShotsGuard(t, "claude/docs-shots-recapture", diff); code == 0 {
		t.Fatal("a branch with no repo facts skipped the guard")
	}
}
