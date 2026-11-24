package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The runner's gh has no `isInMergeQueue` field for `--json` (#2636): the docs-shots
// recapture failed on every main push with "Unknown JSON field". The merge queue
// state is read through GraphQL, where the field exists, so no workflow may ask
// `gh ... --json` for it.
func TestNoWorkflowAsksGhJSONForIsInMergeQueue(t *testing.T) {
	files, err := filepath.Glob("../.github/workflows/*.yml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no workflows found (%v)", err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, "--json") && strings.Contains(line, "isInMergeQueue") {
				t.Errorf("%s:%d asks gh --json for isInMergeQueue, which the runner's gh does not have; use gh api graphql", filepath.Base(f), i+1)
			}
		}
	}
}
