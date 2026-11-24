package scripts

import (
	"os/exec"
	"strings"
	"testing"
)

// These drive scripts/ci-changes.sh, which the changes job in test.yml runs to
// decide which jobs a push needs (#2597). The policy is one place, tested
// here, so a workflow only reads its outputs.

func runChanges(t *testing.T, event, action string, draft bool, files ...string) map[string]string {
	t.Helper()
	cmd := exec.Command("bash", "ci-changes.sh")
	cmd.Env = append(cmd.Environ(), "EVENT="+event, "ACTION="+action, "DRAFT="+map[bool]string{true: "true", false: "false"}[draft])
	cmd.Stdin = strings.NewReader(strings.Join(files, "\n") + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ci-changes.sh: %v\n%s", err, out)
	}
	got := map[string]string{}
	for l := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			t.Fatalf("not key=value: %q in\n%s", l, out)
		}
		got[k] = v
	}
	return got
}

func TestCIChangesSelectsJobsByArea(t *testing.T) {
	cases := []struct {
		name   string
		draft  bool
		files  []string
		wantOn string // the outputs expected true, space separated; every other output is false
	}{
		{"docs only", false, []string{"docs/src/content/docs/operator/notifications.md", "docs/screenshots/baseline/x.png"}, "docs"},
		{"a readme and the screenshots", false, []string{"README.md", ".github/screenshots/1/a.png"}, "docs"},
		{"web only", false, []string{"web/src/pages/Users.tsx"}, "web gen image"},
		{"go only", false, []string{"internal/storage/x.go"}, "go gen image"},
		{"embedded yaml under internal is go", false, []string{"internal/lab/omnibot/default.yaml"}, "go gen image"},
		{"a migration is go", false, []string{"db/migrations/20261124_x.sql"}, "go gen image"},
		{"go and web", false, []string{"cmd/omniglass/main.go", "web/src/a.ts"}, "go web gen image"},
		{"generated docs need gen-drift", false, []string{"docs/src/generated/x.json"}, "docs gen"},
		{"the workflows run everything", false, []string{".github/workflows/test.yml"}, "docs go web gen image"},
		{"the Dockerfile runs everything", false, []string{"Dockerfile"}, "docs go web gen image"},
		{"the Makefile runs everything", false, []string{"Makefile", "docs/a.md"}, "docs go web gen image"},
		{"a path no area claims runs everything", false, []string{"deploy/helm/values.yaml"}, "docs go web gen image"},
		{"a draft runs the unit jobs, never the image", true, []string{"web/src/a.ts", "internal/a.go"}, "go web gen"},
		{"a draft touching CI config still skips the image", true, []string{".github/workflows/test.yml"}, "docs go web gen"},
		{"no file list runs everything", false, nil, "docs go web gen image"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := runChanges(t, "pull_request", "synchronize", c.draft, c.files...)
			on := map[string]bool{}
			for k := range strings.FieldsSeq(c.wantOn) {
				on[k] = true
			}
			for _, k := range []string{"docs", "go", "web", "gen", "image"} {
				want := "false"
				if on[k] {
					want = "true"
				}
				if got[k] != want {
					t.Errorf("%s = %s, want %s (all: %v)", k, got[k], want, got)
				}
			}
		})
	}
}

func TestCIChangesRunsEverythingOffAPullRequestPush(t *testing.T) {
	for _, event := range []string{"push", "merge_group", "workflow_dispatch"} {
		got := runChanges(t, event, "", false, "docs/a.md")
		for _, k := range []string{"docs", "go", "web", "gen", "image"} {
			if got[k] != "true" {
				t.Errorf("%s: %s = %s, want true", event, k, got[k])
			}
		}
	}
}

func TestCIChangesRunsEverythingWhenADraftIsMarkedReady(t *testing.T) {
	got := runChanges(t, "pull_request", "ready_for_review", false, "docs/a.md")
	for _, k := range []string{"docs", "go", "web", "gen", "image"} {
		if got[k] != "true" {
			t.Errorf("%s = %s, want true", k, got[k])
		}
	}
}
