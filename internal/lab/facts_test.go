package lab

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
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
	if !strings.Contains(string(fresh), `"outpost"`) || !strings.Contains(string(fresh), "NET_ADMIN") {
		t.Fatal("the facts lack the outpost and its NET_ADMIN")
	}
	for _, want := range []string{`"cadences"`, `"walkthrough"`, `"probe_every"`, `"30s"`} {
		if !strings.Contains(string(fresh), want) {
			t.Fatalf("the facts lack the cadence profiles (%s)", want)
		}
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
	want := `omniglass component create --label 'Bay 2 Display' --location bay-2 --name bay-2-display --product acme-panel`
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
	// A copied command runs as written: an address's $ and a message's quote
	// reach the CLI rather than being expanded or ending the word.
	quoted, err := SetupCommand("/components/hq.west.$comp.videobar-1/alarms", map[string]any{"message": "Video's $feed lost"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `omniglass component alarm create 'hq.west.$comp.videobar-1' --message 'Video'\''s $feed lost'`; quoted != want {
		t.Fatalf("got  %s\nwant %s", quoted, want)
	}
	for _, bad := range []string{"components", "/components/x", "/", "/auth/me/tokens", "/components//alarms", "/a/b/c/d"} {
		if _, err := SetupCommand(bad, map[string]any{"name": "x"}); err == nil {
			t.Errorf("SetupCommand(%q) made a command, want a refusal", bad)
		}
	}
}

// The topology table names a container's caps as a learner reads them, and
// an uncapped container as none (#1892).
func TestFormatLimits(t *testing.T) {
	for _, tc := range []struct {
		cpus   float64
		memory int64
		want   string
	}{
		{1, 512 << 20, "1 CPU, 512 MiB"},
		{2, 2969 << 20, "2 CPUs, 2969 MiB"},
		{0.5, 0, "0.5 CPUs"},
		{0, 0, ""},
	} {
		if got := FormatLimits(tc.cpus, tc.memory); got != tc.want {
			t.Errorf("FormatLimits(%g, %d) = %q, want %q", tc.cpus, tc.memory, got, tc.want)
		}
	}
}

// What a walkthrough's lab runs beyond the dev fleet (#2362) reaches the lab
// guide, so it can say a walkthrough on the identity provider is a script
// lab's; a walkthrough that names none lists none.
func TestLabFactsCarryWhatAWalkthroughsLabRuns(t *testing.T) {
	b, err := FactsJSON(fstest.MapFS{
		"plain.yaml": {Data: []byte("title: Plain\nsteps: []\n")},
		"sso.yaml":   {Data: []byte("title: SSO\nlab: [sso]\nsteps: []\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Walkthroughs []Walkthrough `json:"walkthroughs"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	for _, w := range f.Walkthroughs {
		got[w.ID] = w.Lab
	}
	if !reflect.DeepEqual(got, map[string][]string{"plain": {}, "sso": {"sso"}}) {
		t.Fatalf("walkthrough labs = %v, want plain none and sso the identity provider", got)
	}
}

// A setup's fault, a command it issues and its waits (#2425: put, a :verb
// post, until, pause) stage a moment, not
// a thing a learner's lab can make with a create, so the facts list only the
// creates and read the rest without failing.
func TestSetupFactsListOnlyTheCreates(t *testing.T) {
	fsys := fstest.MapFS{"w.yaml": {Data: []byte(`title: W
setup:
  - post: /components
    body: { name: bay-2-display, product: boreal-edge-55 }
  - put: /components/bay-2-display/impairment
    body: { unplugged: true }
  - until: { get: /components/bay-2-display/alarms, contains: raised_at }
  - post: /components/bay-2-display/commands:issue
    body: { command_type: reboot }
  - pause: 30
steps:
  - wait: { text: x }
    caption: x
`)}}
	b, err := FactsJSON(fsys)
	if err != nil {
		t.Fatal(err)
	}
	var f Facts
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Walkthroughs) != 1 || len(f.Walkthroughs[0].Setup) != 1 || !strings.Contains(f.Walkthroughs[0].Setup[0], "component create") {
		t.Fatalf("setup facts = %+v, want the one create", f.Walkthroughs)
	}
}
