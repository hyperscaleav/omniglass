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

// A walkthrough's setup is listed for a learner as the CLI command that makes
// it, so a learner's own lab can stage the same scene the recording shows.
func TestSetupCommandIsTheCLICreate(t *testing.T) {
	got, err := SetupCommand("/components", map[string]any{
		"name": "bay-2-display", "label": "Bay 2 Display", "product": "acme-panel", "location": "bay-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `omniglass component create --label "Bay 2 Display" --location bay-2 --name bay-2-display --product acme-panel`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	nested, err := SetupCommand("/components/panel-2/alarms", map[string]any{"severity": "high", "dedup_key": "walkthrough"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `omniglass component alarm create panel-2 --dedup-key walkthrough --severity high`; nested != want {
		t.Fatalf("got  %s\nwant %s", nested, want)
	}
	for _, bad := range []string{"components", "/components/x", "/", "/auth/me/tokens", "/components//alarms", "/a/b/c/d"} {
		if _, err := SetupCommand(bad, map[string]any{"name": "x"}); err == nil {
			t.Errorf("SetupCommand(%q) made a command, want a refusal", bad)
		}
	}
}
