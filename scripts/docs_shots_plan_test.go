package scripts

import (
	"os/exec"
	"strings"
	"testing"
)

// These drive scripts/docs-shots-plan.sh (#2602): which shots the recapture job
// commits, and which branch it pushes to.

func docsShotsPlan(t *testing.T, stdin string, args ...string) (string, int) {
	t.Helper()
	requireTools(t)
	cmd := exec.Command("bash", append([]string{"docs-shots-plan.sh"}, args...)...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.Output()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return string(out), code
}

func TestDocsShotsPlanCommitsOnlyChangedBaselinesAndTheirTwins(t *testing.T) {
	status := " M docs/screenshots/baseline/fleet-history.png\n" +
		" M docs/public/screenshots/fleet-history.png\n" +
		" M docs/public/screenshots/alarm-queue.png\n" +
		" M docs/public/screenshots/audit.png\n" +
		"?? docs/screenshots/baseline/new-id.png\n" +
		"?? docs/public/screenshots/new-id.png\n"
	out, code := docsShotsPlan(t, status, "select")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	want := "docs/public/screenshots/fleet-history.png\ndocs/public/screenshots/new-id.png\n" +
		"docs/screenshots/baseline/fleet-history.png\ndocs/screenshots/baseline/new-id.png\n"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

func TestDocsShotsPlanNoBaselineChangeCommitsNothing(t *testing.T) {
	status := " M docs/public/screenshots/alarm-queue.png\n M docs/public/screenshots/audit.png\n"
	if out, code := docsShotsPlan(t, status, "select"); code != 0 || out != "" {
		t.Fatalf("exit %d out %q", code, out)
	}
	if out, code := docsShotsPlan(t, "", "select"); code != 0 || out != "" {
		t.Fatalf("empty: exit %d out %q", code, out)
	}
}

func TestDocsShotsPlanKeepsADeletedBaselineAndItsTwin(t *testing.T) {
	out, _ := docsShotsPlan(t, " D docs/screenshots/baseline/gone.png\n", "select")
	if out != "docs/public/screenshots/gone.png\ndocs/screenshots/baseline/gone.png\n" {
		t.Fatalf("got %q", out)
	}
}

func TestDocsShotsPlanBranch(t *testing.T) {
	sha := "620f8e2312345678"
	for _, c := range []struct{ base, fresh, want string }{
		{"main", "false", "claude/docs-shots-recapture"},
		{"main", "true", "claude/docs-shots-recapture-620f8e2"},
		{"release/1.2", "false", "claude/docs-shots-recapture-release-1.2"},
		{"release/1.2", "true", "claude/docs-shots-recapture-release-1.2-620f8e2"},
	} {
		out, code := docsShotsPlan(t, "", "branch", c.base, sha, c.fresh)
		if code != 0 || strings.TrimSpace(out) != c.want {
			t.Errorf("%s fresh=%s: exit %d got %q want %q", c.base, c.fresh, code, out, c.want)
		}
	}
}

func TestDocsShotsPlanRejectsUnknownCommand(t *testing.T) {
	if _, code := docsShotsPlan(t, "", "bogus"); code != 2 {
		t.Fatalf("exit %d", code)
	}
}
