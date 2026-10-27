package lab

import (
	"os"
	"strings"
	"testing"
)

// TestLabFactsMatchCommitted is the drift gate for the lab guide's generated
// topology table and walkthrough list: a change to the planner, or a
// walkthrough added or retitled, without make gen fails here.
func TestLabFactsMatchCommitted(t *testing.T) {
	fresh, err := FactsJSON(os.DirFS("../../web/e2e/walkthroughs"))
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile("../../docs/src/generated/lab.json")
	if err != nil {
		t.Fatalf("read committed lab.json (run make gen and commit it): %v", err)
	}
	if string(committed) != string(fresh) {
		t.Error("docs/src/generated/lab.json drifted from the planner or the walkthroughs: run make gen and commit the result")
	}
	if !strings.Contains(string(fresh), `"node"`) || !strings.Contains(string(fresh), "NET_ADMIN") {
		t.Fatal("the facts lack the node and its NET_ADMIN")
	}
}
