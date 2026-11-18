package scripts

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These drive scripts/agent-review.sh, the split of agent-review into
// parallel lenses (#2211): the catalog is dealt across the catalog lenses with
// none dropped, and the lenses' findings become one review whose event is the
// verdict agent-trust.sh reads.

func agentReview(t *testing.T, stdin string, args ...string) (string, int) {
	t.Helper()
	requireTools(t)
	cmd := exec.Command("bash", append([]string{"agent-review.sh"}, args...)...)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out.String()), code
}

type lens struct {
	Name string `json:"name"`
	Task string `json:"task"`
}

func catalog(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("## x (live)\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestAgentReviewLensesDealEveryCatalogEntryOnce(t *testing.T) {
	names := []string{"README.md"}
	for _, c := range "abcdefg" {
		names = append(names, string(c)+"-entry.md")
	}
	out, code := agentReview(t, "", "lenses", catalog(t, names...))
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var lenses []lens
	if err := json.Unmarshal([]byte(out), &lenses); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	var got []string
	for _, l := range lenses {
		got = append(got, l.Name)
	}
	if strings.Join(got, ",") != "catalog-1,catalog-2,catalog-3,invariants,neighbor" {
		t.Fatalf("lenses = %v", got)
	}
	for _, c := range "abcdefg" {
		entry := string(c) + "-entry.md"
		seen := 0
		for _, l := range lenses[:3] {
			if strings.Contains(l.Task, entry) {
				seen++
			}
		}
		if seen != 1 {
			t.Errorf("%s is in %d catalog lenses, want 1", entry, seen)
		}
	}
	for _, l := range lenses {
		if strings.Contains(l.Task, "README.md") {
			t.Errorf("%s sweeps the README", l.Name)
		}
	}
}

func TestAgentReviewLensesSweepTheRealCatalog(t *testing.T) {
	dir := filepath.Join("..", ".claude", "skills", "adversarial-review", "references", "anti-patterns")
	out, code := agentReview(t, "", "lenses", dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	var lenses []lens
	if err := json.Unmarshal([]byte(out), &lenses); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == "README.md" {
			continue
		}
		found := false
		for _, l := range lenses {
			if strings.Contains(l.Task, e.Name()) {
				found = true
			}
		}
		if !found {
			t.Errorf("no lens sweeps %s", e.Name())
		}
	}
}

func TestAgentReviewLensesRefuseAnOddCatalogName(t *testing.T) {
	for _, bad := range []string{"Upper.md", "has space.md", "-dash.md"} {
		if _, code := agentReview(t, "", "lenses", catalog(t, "ok.md", bad)); code != 2 {
			t.Errorf("%q: exit %d, want 2", bad, code)
		}
	}
	if _, code := agentReview(t, "", "lenses", catalog(t, "README.md")); code != 2 {
		t.Errorf("an empty catalog: exit %d, want 2", code)
	}
}

type review struct {
	CommitID string `json:"commit_id"`
	Event    string `json:"event"`
	Body     string `json:"body"`
	Comments []struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		Side string `json:"side"`
		Body string `json:"body"`
	} `json:"comments"`
}

func reviewOf(t *testing.T, results string) review {
	t.Helper()
	out, code := agentReview(t, results, "review")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	var r review
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return r
}

func TestAgentReviewRequestsChangesWhenAnyLensBlocks(t *testing.T) {
	r := reviewOf(t, `{"head_sha":"h2","expected":["catalog-1","neighbor"],"lenses":[
		{"name":"catalog-1","summary":"one live cue","findings":[{"severity":"blocking","path":"internal/x.go","line":12,"rule":"mocked-sut","failure":"the test mocks the store","fix":"use testcontainers"}]},
		{"name":"neighbor","summary":"docs agree","findings":[]}]}`)
	if r.Event != "REQUEST_CHANGES" || r.CommitID != "h2" {
		t.Fatalf("event %s on %s, want REQUEST_CHANGES on h2", r.Event, r.CommitID)
	}
	if len(r.Comments) != 1 || r.Comments[0].Path != "internal/x.go" || r.Comments[0].Line != 12 || r.Comments[0].Side != "RIGHT" {
		t.Fatalf("comments = %+v", r.Comments)
	}
	if !strings.Contains(r.Comments[0].Body, "the test mocks the store") || !strings.Contains(r.Comments[0].Body, "catalog-1") {
		t.Fatalf("comment body = %q", r.Comments[0].Body)
	}
	if !strings.Contains(r.Body, "1 blocking, 0 advisory") || !strings.Contains(r.Body, "neighbor: docs agree") {
		t.Fatalf("body = %q", r.Body)
	}
}

func TestAgentReviewCommentsWhenNothingBlocks(t *testing.T) {
	clean := reviewOf(t, `{"head_sha":"h","expected":["invariants"],"lenses":[{"name":"invariants","summary":"swept","findings":[]}]}`)
	if clean.Event != "COMMENT" || !strings.HasPrefix(clean.Body, "agent-review: swept, nothing survived") {
		t.Fatalf("clean: %s %q", clean.Event, clean.Body)
	}
	advisory := reviewOf(t, `{"head_sha":"h","expected":["invariants"],"lenses":[{"name":"invariants","findings":[{"severity":"advisory","path":"a.go","line":1,"rule":"r","failure":"f","fix":"x"}]}]}`)
	if advisory.Event != "COMMENT" {
		t.Fatalf("advisory only: %s, want COMMENT", advisory.Event)
	}
}

func TestAgentReviewTreatsAnUnknownSeverityAsBlocking(t *testing.T) {
	r := reviewOf(t, `{"head_sha":"h","expected":["a"],"lenses":[{"name":"a","findings":[{"severity":"nit","path":"a.go","line":1,"rule":"r","failure":"f","fix":"x"},{"path":"b.go","line":2}]}]}`)
	if r.Event != "REQUEST_CHANGES" || !strings.Contains(r.Body, "2 blocking") {
		t.Fatalf("%s %q", r.Event, r.Body)
	}
}

func TestAgentReviewPutsUnplaceableFindingsInTheBody(t *testing.T) {
	results := `{"head_sha":"h","expected":["a"],"lenses":[{"name":"a","findings":[
		{"severity":"advisory","path":"docs/x.md","line":0,"rule":"falsified-neighbor-claim","failure":"says retries are unbounded","fix":"x"},
		{"severity":"advisory","path":"a.go","line":4,"rule":"r","failure":"on a line","fix":"x"}]}]}`
	r := reviewOf(t, results)
	if len(r.Comments) != 1 || r.Comments[0].Path != "a.go" {
		t.Fatalf("comments = %+v", r.Comments)
	}
	if !strings.Contains(r.Body, "`docs/x.md`") || !strings.Contains(r.Body, "says retries are unbounded") {
		t.Fatalf("body = %q", r.Body)
	}
	// With inline off (GitHub refused a line), every finding is in the body.
	flat := reviewOf(t, strings.Replace(results, `"head_sha":"h"`, `"head_sha":"h","inline":false`, 1))
	if len(flat.Comments) != 0 || !strings.Contains(flat.Body, "`a.go:4`") || !strings.Contains(flat.Body, "on a line") {
		t.Fatalf("flat = %+v", flat)
	}
}

func TestAgentReviewRefusesAMissingLensAndBadInput(t *testing.T) {
	if _, code := agentReview(t, `{"head_sha":"h","expected":["a","b"],"lenses":[{"name":"a","findings":[]}]}`, "review"); code != 3 {
		t.Fatalf("a lens that returned nothing: exit %d, want 3", code)
	}
	for _, bad := range []string{"not json", `{"expected":["a"],"lenses":[]}`, `{"head_sha":"h","lenses":[]}`} {
		if _, code := agentReview(t, bad, "review"); code != 2 {
			t.Errorf("%q: exit %d, want 2", bad, code)
		}
	}
	if _, code := agentReview(t, "", "merge"); code != 64 {
		t.Fatalf("bad usage: exit %d, want 64", code)
	}
}
