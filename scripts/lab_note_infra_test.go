package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// lab_note_infra (web/e2e/lib/lab.sh) leaves a mark when the lab did not come up
// because its registry refused an image pull for its rate limit (#2020), so the
// run:demo comment reports an infrastructure failure and not a broken demo. It
// writes nothing for any other reason the lab did not come up.
func labNoteInfra(t *testing.T, logText string) (mark string, wrote bool) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "lab-up.log")
	markPath := filepath.Join(dir, "infra")
	if err := os.WriteFile(logPath, []byte(logText), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-c", `set -e; . ../web/e2e/lib/lab.sh; lab_note_infra "$1" "$2"`, "bash", markPath, logPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("lab_note_infra: %v\n%s", err, out)
	}
	b, err := os.ReadFile(markPath)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

func TestLabNoteInfraMarksARegistryRateLimit(t *testing.T) {
	line := "error: pull postgres:18: registry rate limit: postgres:18 is rate limited by its registry (HTTP 429) after 4 retries: 429 Too Many Requests"
	mark, wrote := labNoteInfra(t, "lab ogdemo-1: image      pulling postgres:18\nlab ogdemo-1: "+line+"\nlab ogdemo-1: The lab did not come up\n")
	if !wrote || !strings.Contains(mark, "registry rate limit") {
		t.Fatalf("mark %q (written %v), want the rate limit line", mark, wrote)
	}
}

func TestLabNoteInfraLeavesNoMarkForAnotherFailure(t *testing.T) {
	for _, logText := range []string{
		"lab x: error: pull postgres:18: manifest unknown\n",
		"lab x: the lab did not come up: port 8080 is in use\n",
		"",
	} {
		if mark, wrote := labNoteInfra(t, logText); wrote {
			t.Errorf("log %q left a mark %q, want none", logText, mark)
		}
	}
}
