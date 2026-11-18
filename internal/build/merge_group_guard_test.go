package build

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// requiredChecks are the check names the rulesets on main require, across
// both of them ("Protect main" and "main: architect approval"). The rulesets
// live in the repository's settings, not the tree, so this list is kept in
// step with them by hand: a check made required there is added here in the
// same change (#2181).
var requiredChecks = []string{"e2e", "gen-drift", "lint", "smoke-lab", "test"}

// workflowFile is the part of a GitHub Actions workflow the guard reads: its
// triggers and the check name each job reports.
type workflowFile struct {
	On   yaml.Node `yaml:"on"`
	Jobs map[string]struct {
		Name string `yaml:"name"`
	} `yaml:"jobs"`
}

// workflowEvents returns the events a workflow's `on:` names, in any of its
// three shapes: a bare event (`on: push`), a list (`on: [push, pull_request]`)
// or a map of event to its filters.
func workflowEvents(on yaml.Node) []string {
	switch on.Kind {
	case yaml.ScalarNode:
		return []string{on.Value}
	case yaml.SequenceNode:
		var events []string
		for _, n := range on.Content {
			events = append(events, n.Value)
		}
		return events
	case yaml.MappingNode:
		var events []string
		for i := 0; i < len(on.Content); i += 2 {
			events = append(events, on.Content[i].Value)
		}
		return events
	}
	return nil
}

// jobCheckName is the name a job's check run reports: its `name:` when set,
// else its id.
func jobCheckName(id, name string) string {
	if name != "" {
		return name
	}
	return id
}

// mergeGroupGaps reads workflows (file name to contents) and returns, sorted,
// every "<file>: <check>" where a job reporting a required check sits in a
// workflow with no merge_group trigger, and every "missing: <check>" for a
// required check no workflow reports at all. A queue entry waits forever for
// a required check that never reports on it, so either gap stalls the merge
// queue.
func mergeGroupGaps(workflows map[string][]byte, required []string) ([]string, error) {
	want := map[string]bool{}
	for _, c := range required {
		want[c] = true
	}
	reported := map[string]bool{}
	var gaps []string
	for file, body := range workflows {
		var wf workflowFile
		if err := yaml.Unmarshal(body, &wf); err != nil {
			return nil, err
		}
		mergeGroup := false
		for _, e := range workflowEvents(wf.On) {
			if e == "merge_group" {
				mergeGroup = true
			}
		}
		for id, job := range wf.Jobs {
			check := jobCheckName(id, job.Name)
			if !want[check] {
				continue
			}
			reported[check] = true
			if !mergeGroup {
				gaps = append(gaps, file+": "+check)
			}
		}
	}
	for _, c := range required {
		if !reported[c] {
			gaps = append(gaps, "missing: "+c)
		}
	}
	sort.Strings(gaps)
	return gaps, nil
}

func TestWorkflowEventsReadsEveryShapeOfOn(t *testing.T) {
	cases := map[string][]string{
		"on: push\njobs: {}\n":                {"push"},
		"on: [push, merge_group]\njobs: {}\n": {"push", "merge_group"},
		"on:\n  pull_request:\n  merge_group:\n    types: [checks_requested]\njobs: {}\n": {"pull_request", "merge_group"},
	}
	for src, want := range cases {
		var wf workflowFile
		if err := yaml.Unmarshal([]byte(src), &wf); err != nil {
			t.Fatalf("unmarshal %q: %v", src, err)
		}
		if got := workflowEvents(wf.On); !reflect.DeepEqual(got, want) {
			t.Errorf("workflowEvents(%q) = %v, want %v", src, got, want)
		}
	}
}

func TestMergeGroupGapsFindsEveryWayTheQueueStalls(t *testing.T) {
	workflows := map[string][]byte{
		// Reports a required check by job id, and runs in the queue.
		"ok.yml": []byte("on:\n  pull_request:\n  merge_group:\njobs:\n  test:\n    runs-on: x\n"),
		// Reports a required check by job id, never in the queue.
		"by-id.yml": []byte("on:\n  pull_request:\njobs:\n  gen-drift:\n    runs-on: x\n"),
		// Reports a required check by display name, never in the queue.
		"by-name.yml": []byte("on: [pull_request]\njobs:\n  title:\n    name: lint\n    runs-on: x\n"),
		// Not a required check: no merge_group needed.
		"other.yml": []byte("on: pull_request\njobs:\n  docs:\n    runs-on: x\n"),
	}
	got, err := mergeGroupGaps(workflows, []string{"e2e", "gen-drift", "lint", "test"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"by-id.yml: gen-drift", "by-name.yml: lint", "missing: e2e"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeGroupGaps = %v, want %v", got, want)
	}
}

// TestEveryRequiredCheckRunsInTheMergeQueue reads .github/workflows/ and fails
// when a workflow that reports a required check lacks a merge_group trigger, or
// when no workflow reports one. The merge queue (#2181) tests each PR on top
// of main and merges it once the required checks pass there, so a required
// check that never reports on a queue entry holds every PR behind it.
func TestEveryRequiredCheckRunsInTheMergeQueue(t *testing.T) {
	dir := filepath.Join("..", "..", ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	workflows := map[string][]byte{}
	for _, e := range entries {
		if e.IsDir() || !(strings.HasSuffix(e.Name(), ".yml") || strings.HasSuffix(e.Name(), ".yaml")) {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		workflows[e.Name()] = body
	}
	gaps, err := mergeGroupGaps(workflows, requiredChecks)
	if err != nil {
		t.Fatal(err)
	}
	if len(gaps) > 0 {
		t.Fatalf("required checks that would never report in the merge queue (add `merge_group:` to the workflow, or fix requiredChecks):\n  %s",
			strings.Join(gaps, "\n  "))
	}
}
