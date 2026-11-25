package scripts

import (
	"os"
	"strings"
	"testing"
)

// A draft PR runs none of the heavy path-filtered checks (#2309): each of these
// workflows guards its job on the draft flag and the run:test opt-in, and
// listens for ready_for_review (to run what the draft skipped) and labeled (to
// start the opt-in). Cheap metadata checks (pr-title, demo-tag) are not in the
// list: they run on a draft.
func TestHeavyPullRequestWorkflowsSkipDrafts(t *testing.T) {
	for _, wf := range []string{"docs-build", "docs-shots", "goreleaser", "migrations"} {
		b, err := os.ReadFile("../.github/workflows/" + wf + ".yml")
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		for _, want := range []string{
			"types: [opened, synchronize, reopened, ready_for_review, labeled]",
			"!github.event.pull_request.draft",
			"'run:test'",
			"github.event_name != 'pull_request'",
		} {
			if !strings.Contains(s, want) {
				t.Errorf("%s.yml lacks %q: a draft would run it, or could not opt in or be run when ready", wf, want)
			}
		}
	}
}
