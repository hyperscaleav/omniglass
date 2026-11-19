package scripts

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// These drive scripts/agent-review.sh, the split of agent-review into
// parallel lenses (#2211) and the compact output and incremental re-review
// (#2224): the catalog is dealt across the catalog lenses with none dropped,
// the lenses' findings become one terse review, duplicate findings are dropped,
// and small diffs run fewer lenses and fewer turns.

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
	if strings.Join(got, ",") != "catalog-1,catalog-2,catalog-3,catalog-4,catalog-5,catalog-6,invariants,neighbor" {
		t.Fatalf("lenses = %v", got)
	}
	for _, c := range "abcdefg" {
		entry := string(c) + "-entry.md"
		seen := 0
		for _, l := range lenses[:len(lenses)-2] {
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

func TestAgentReviewLensesShardTheRealCatalogEvenly(t *testing.T) {
	dir := filepath.Join("..", ".claude", "skills", "adversarial-review", "references", "anti-patterns")
	out, code := agentReview(t, "", "lenses", dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	var lenses []lens
	if err := json.Unmarshal([]byte(out), &lenses); err != nil {
		t.Fatal(err)
	}
	min, max := -1, 0
	for _, l := range lenses {
		if !strings.HasPrefix(l.Name, "catalog-") {
			continue
		}
		n := strings.Count(l.Task, ".md")
		if min < 0 || n < min {
			min = n
		}
		if n > max {
			max = n
		}
	}
	if max-min > 1 {
		t.Fatalf("catalog shards run %d to %d entries; the slowest shard sets the review's time", min, max)
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

// TestAgentReviewLensesFilterByDiff checks that a diff that touches no code
// returns neighbor only, and one that touches code returns all lenses.
func TestAgentReviewLensesFilterByDiff(t *testing.T) {
	dir := catalog(t, "a-entry.md", "b-entry.md")

	// A diff with only .md files: neighbor only.
	docsDiff := filepath.Join(t.TempDir(), "docs.diff")
	if err := os.WriteFile(docsDiff, []byte("+++ b/docs/page.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := agentReview(t, "", "lenses", dir, docsDiff)
	if code != 0 {
		t.Fatalf("docs diff: exit %d: %s", code, out)
	}
	var docsLenses []lens
	if err := json.Unmarshal([]byte(out), &docsLenses); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if len(docsLenses) != 1 || docsLenses[0].Name != "neighbor" {
		t.Fatalf("docs-only diff: lenses = %v, want [neighbor]", docsLenses)
	}

	// A diff with a .go file: all lenses (catalog shards + invariants + neighbor).
	goDiff := filepath.Join(t.TempDir(), "go.diff")
	if err := os.WriteFile(goDiff, []byte("+++ b/internal/x.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out2, code2 := agentReview(t, "", "lenses", dir, goDiff)
	if code2 != 0 {
		t.Fatalf("go diff: exit %d: %s", code2, out2)
	}
	var goLenses []lens
	if err := json.Unmarshal([]byte(out2), &goLenses); err != nil {
		t.Fatalf("%v: %s", err, out2)
	}
	var goNames []string
	for _, l := range goLenses {
		goNames = append(goNames, l.Name)
	}
	joined := strings.Join(goNames, ",")
	if !strings.Contains(joined, "invariants") || !strings.Contains(joined, "neighbor") || !strings.Contains(joined, "catalog-") {
		t.Fatalf("go diff: lenses = %v, want catalog+invariants+neighbor", goNames)
	}

	// A diff with a .tsx file: all lenses.
	webDiff := filepath.Join(t.TempDir(), "web.diff")
	if err := os.WriteFile(webDiff, []byte("+++ b/web/src/App.tsx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out3, code3 := agentReview(t, "", "lenses", dir, webDiff)
	if code3 != 0 {
		t.Fatalf("tsx diff: exit %d: %s", code3, out3)
	}
	if !strings.Contains(out3, "invariants") {
		t.Fatalf("tsx diff: invariants lens missing: %s", out3)
	}

	// Shell, SQL, the workflows and a deleted Go file are code too: the
	// invariants lens found PR #2226's one real problem in a bash script.
	for _, d := range []string{
		"diff --git a/scripts/x.sh b/scripts/x.sh\n+++ b/scripts/x.sh\n",
		"+++ b/internal/storage/migrations/0042_x.sql\n",
		"diff --git a/.github/workflows/test.yml b/.github/workflows/test.yml\n",
		"diff --git a/internal/x.go b/internal/x.go\ndeleted file mode 100644\n--- a/internal/x.go\n+++ /dev/null\n",
	} {
		f := filepath.Join(t.TempDir(), "x.diff")
		if err := os.WriteFile(f, []byte(d), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, code := agentReview(t, "", "lenses", dir, f); code != 0 || !strings.Contains(out, "invariants") {
			t.Fatalf("%q: exit %d, lenses %s, want every lens", d, code, out)
		}
	}
	// An empty diff (a re-review after a merge from main) runs neighbor alone.
	empty := filepath.Join(t.TempDir(), "empty.diff")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	out5, _ := agentReview(t, "", "lenses", dir, empty)
	var emptyLenses []lens
	if err := json.Unmarshal([]byte(out5), &emptyLenses); err != nil || len(emptyLenses) != 1 || emptyLenses[0].Name != "neighbor" {
		t.Fatalf("empty diff: lenses %s, want neighbor alone", out5)
	}

	// No diff file: all lenses (backward compat).
	out4, code4 := agentReview(t, "", "lenses", dir)
	if code4 != 0 {
		t.Fatalf("no diff: exit %d: %s", code4, out4)
	}
	if !strings.Contains(out4, "invariants") {
		t.Fatalf("no diff: invariants lens missing: %s", out4)
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
	// Verdict line uses · separator; per-lens summary is in the details block.
	if !strings.Contains(r.Body, "1 blocking · 0 advisory") || !strings.Contains(r.Body, "docs agree") {
		t.Fatalf("body = %q", r.Body)
	}
}

func TestAgentReviewCommentsWhenNothingBlocks(t *testing.T) {
	clean := reviewOf(t, `{"head_sha":"h","expected":["invariants"],"lenses":[{"name":"invariants","summary":"swept","findings":[]}]}`)
	if clean.Event != "COMMENT" || !strings.HasPrefix(clean.Body, "✅ Ship · nothing found") {
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

// GitHub refuses Request changes from the App that wrote the PR, so on its own
// PR the review is a comment that still opens with the blocking count, which
// agent-trust.sh verdict reads to block when needed.
func TestAgentReviewOnlyCommentsOnItsOwnAppsPR(t *testing.T) {
	r := reviewOf(t, `{"head_sha":"h","own_pr":true,"expected":["a"],"lenses":[{"name":"a","findings":[{"severity":"blocking","path":"a.go","line":1,"rule":"r","failure":"f","fix":"x"}]}]}`)
	if r.Event != "COMMENT" || !strings.HasPrefix(r.Body, "❌ Changes requested · 1 blocking · 0 advisory") {
		t.Fatalf("own PR: %s %q, want a COMMENT that opens with the blocking count", r.Event, r.Body)
	}
}

// TestAgentReviewBodyVerdictLine checks that the body opens with the compact
// verdict line: ✅ Ship, ❌ Changes requested, or ⚠️ advisory-only.
func TestAgentReviewBodyVerdictLine(t *testing.T) {
	clean := reviewOf(t, `{"head_sha":"h","expected":["a"],"lenses":[{"name":"a","summary":"swept","findings":[]}]}`)
	if !strings.HasPrefix(clean.Body, "✅ Ship · nothing found") {
		t.Fatalf("clean body = %q", clean.Body)
	}

	blocking := reviewOf(t, `{"head_sha":"h","expected":["a"],"lenses":[{"name":"a","findings":[
		{"severity":"blocking","path":"x.go","line":1,"rule":"r","failure":"f","fix":"x"}]}]}`)
	if !strings.HasPrefix(blocking.Body, "❌ Changes requested · 1 blocking · 0 advisory") {
		t.Fatalf("blocking body = %q", blocking.Body)
	}

	advisory := reviewOf(t, `{"head_sha":"h","expected":["a"],"lenses":[{"name":"a","findings":[
		{"severity":"advisory","path":"x.go","line":0,"rule":"r","failure":"f","fix":"x"}]}]}`)
	if !strings.HasPrefix(advisory.Body, "⚠️ 1 advisory") {
		t.Fatalf("advisory body = %q", advisory.Body)
	}
}

// TestAgentReviewBodyLensStrip checks that the body includes a lens strip with
// ✅/⚠️/❌ per lens separated by ·.
func TestAgentReviewBodyLensStrip(t *testing.T) {
	r := reviewOf(t, `{"head_sha":"h","expected":["a","b","c"],"lenses":[
		{"name":"a","findings":[]},
		{"name":"b","findings":[{"severity":"advisory","path":"x.go","line":0,"rule":"r","failure":"f","fix":"x"}]},
		{"name":"c","findings":[{"severity":"blocking","path":"x.go","line":0,"rule":"r","failure":"f","fix":"x"}]}
	]}`)
	if !strings.Contains(r.Body, "✅ a") || !strings.Contains(r.Body, "⚠️ b") || !strings.Contains(r.Body, "❌ c") {
		t.Fatalf("body = %q", r.Body)
	}

	// All clean: joined with ·.
	allClean := reviewOf(t, `{"head_sha":"h","expected":["x","y"],"lenses":[{"name":"x","findings":[]},{"name":"y","findings":[]}]}`)
	if !strings.Contains(allClean.Body, "✅ x · ✅ y") {
		t.Fatalf("clean strip = %q", allClean.Body)
	}
}

// TestAgentReviewDetailsBlock checks that failure/fix and per-lens notes go in
// a collapsed <details> block, not in the top-level body prose.
func TestAgentReviewDetailsBlock(t *testing.T) {
	r := reviewOf(t, `{"head_sha":"h","expected":["a"],"lenses":[{
		"name":"a","summary":"one finding",
		"findings":[{"severity":"advisory","path":"x.md","line":0,"rule":"r","title":"The title","failure":"the failure text","fix":"the fix text"}]
	}]}`)
	if !strings.Contains(r.Body, "<details>") || !strings.Contains(r.Body, "</details>") {
		t.Fatalf("no details block: %q", r.Body)
	}
	if !strings.Contains(r.Body, "one finding") {
		t.Fatalf("per-lens summary not in details: %q", r.Body)
	}
	if !strings.Contains(r.Body, "the failure text") || !strings.Contains(r.Body, "the fix text") {
		t.Fatalf("failure/fix not in details: %q", r.Body)
	}
}

// TestAgentReviewInlineCommentTitle checks that inline comments open with a
// bold title (from the title field, or rule if absent) behind the severity's
// icon, and have reasoning in a <details> block.
func TestAgentReviewInlineCommentTitle(t *testing.T) {
	// With title field present: comment opens with that title.
	withTitle := reviewOf(t, `{"head_sha":"h","expected":["a"],"lenses":[{"name":"a","findings":[
		{"severity":"blocking","path":"x.go","line":5,"rule":"mocked-sut","title":"Use testcontainers","failure":"mocks the db","fix":"use real db"}
	]}]}`)
	if len(withTitle.Comments) != 1 || !strings.HasPrefix(withTitle.Comments[0].Body, "**❌ Use testcontainers**") {
		t.Fatalf("title field: comment body = %q", withTitle.Comments[0].Body)
	}
	if !strings.Contains(withTitle.Comments[0].Body, "<details>") || !strings.Contains(withTitle.Comments[0].Body, "mocks the db") {
		t.Fatalf("details block: comment body = %q", withTitle.Comments[0].Body)
	}

	// Without title field: falls back to rule.
	noTitle := reviewOf(t, `{"head_sha":"h","expected":["a"],"lenses":[{"name":"a","findings":[
		{"severity":"blocking","path":"x.go","line":5,"rule":"mocked-sut","failure":"mocks the db","fix":"use real db"}
	]}]}`)
	if len(noTitle.Comments) != 1 || !strings.HasPrefix(noTitle.Comments[0].Body, "**❌ mocked-sut**") {
		t.Fatalf("rule fallback: comment body = %q", noTitle.Comments[0].Body)
	}
}

// TestAgentReviewDeduplicatesOpenFindings checks that a finding an open
// reviewer thread already carries is not posted again, but the thread still
// counts in the verdict, so a blocker that was never fixed keeps the PR blocked
// (the trust bypass the invariants lens found on PR #2226).
func TestAgentReviewDeduplicatesOpenFindings(t *testing.T) {
	results := `{
		"head_sha":"h","expected":["a"],
		"open_threads":[{"path":"x.go","line":6,"start_line":4,"rule":"r","severity":"blocking","lens":"a","title":"Still broken"}],
		"lenses":[{"name":"a","findings":[
			{"severity":"blocking","path":"x.go","line":5,"rule":"r","failure":"f1","fix":"x1"},
			{"severity":"advisory","path":"y.go","line":3,"rule":"s","failure":"f2","fix":"x2"}
		]}]}`
	r := reviewOf(t, results)
	if r.Event != "REQUEST_CHANGES" {
		t.Fatalf("event = %s, want REQUEST_CHANGES: the open blocking thread still counts", r.Event)
	}
	if len(r.Comments) != 1 || r.Comments[0].Path != "y.go" || r.Comments[0].Line != 3 {
		t.Fatalf("comments = %+v, want only y.go:3 (x.go:5 is already on a thread)", r.Comments)
	}
	if !strings.HasPrefix(r.Body, "❌ Changes requested · 1 blocking · 1 advisory") {
		t.Fatalf("body = %q, want 1 blocking (carried) and 1 advisory", r.Body)
	}
	if !strings.Contains(r.Body, "Still open from earlier reviews:\n↪ ❌ `x.go:6` Still broken") {
		t.Fatalf("body does not list the carried thread: %q", r.Body)
	}
	if !strings.Contains(r.Body, "❌ a") {
		t.Fatalf("lens strip should show the carried blocker on its lens: %q", r.Body)
	}

	// The same finding on another rule, or outside the thread's lines, is new.
	for _, f := range []string{
		`{"severity":"blocking","path":"x.go","line":5,"rule":"other","failure":"f","fix":"x"}`,
		`{"severity":"blocking","path":"x.go","line":9,"rule":"r","failure":"f","fix":"x"}`,
	} {
		r := reviewOf(t, `{"head_sha":"h","expected":["a"],
			"open_threads":[{"path":"x.go","line":6,"start_line":4,"rule":"r","severity":"blocking"}],
			"lenses":[{"name":"a","findings":[`+f+`]}]}`)
		if len(r.Comments) != 1 || !strings.HasPrefix(r.Body, "❌ Changes requested · 2 blocking") {
			t.Fatalf("%s: comments %d, body %q", f, len(r.Comments), r.Body)
		}
	}
}

// A re-review that reads only the new changes never sees an old blocker, so
// the open thread alone has to keep the PR blocked, on the agents' own PRs too.
func TestAgentReviewCarriesAnOpenBlockerNoLensRaised(t *testing.T) {
	r := reviewOf(t, `{"head_sha":"h","own_pr":true,"expected":["neighbor"],
		"open_threads":[{"path":"x.go","line":5,"rule":"r","severity":"blocking","lens":"invariants","title":"t",
			"url":"https://github.com/o/r/pull/7#discussion_r42"}],
		"lenses":[{"name":"neighbor","summary":"docs agree","findings":[]}]}`)
	if r.Event != "COMMENT" || !strings.HasPrefix(r.Body, "❌ Changes requested · 1 blocking · 0 advisory") {
		t.Fatalf("%s %q", r.Event, r.Body)
	}
	if !strings.Contains(r.Body, "↪ ❌ [`x.go:5`](https://github.com/o/r/pull/7#discussion_r42) t") {
		t.Fatalf("carried line does not link its thread: %q", r.Body)
	}
	// Only a github.com thread URL is linked.
	odd := reviewOf(t, `{"head_sha":"h","expected":["a"],
		"open_threads":[{"path":"x.go","line":5,"rule":"r","severity":"advisory","title":"t","url":"https://evil.example/x"}],
		"lenses":[{"name":"a","findings":[]}]}`)
	if strings.Contains(odd.Body, "evil.example") || !strings.HasPrefix(odd.Body, "⚠️ 1 advisory") {
		t.Fatalf("odd url: %q", odd.Body)
	}
}

// Each inline comment ends with a marker that threads reads back, and the body
// ends with the count of blocking findings that have no thread.
func TestAgentReviewMarksWhatItPosts(t *testing.T) {
	results := `{"head_sha":"h","expected":["catalog-1"],"lenses":[{"name":"catalog-1","findings":[
		{"severity":"blocking","path":"x.go","line":5,"rule":"Mocked SUT --> x","failure":"f","fix":"x"},
		{"severity":"blocking","path":"docs/a.md","line":0,"rule":"r","failure":"f","fix":"x"}]}]}`
	r := reviewOf(t, results)
	if len(r.Comments) != 1 || !strings.HasSuffix(r.Comments[0].Body, "<!-- agent-review severity=blocking lens=catalog-1 rule=mocked-sut-x -->") {
		t.Fatalf("inline marker: %+v", r.Comments)
	}
	if !strings.HasSuffix(r.Body, "<!-- agent-review v2 loose-blocking=1 -->") {
		t.Fatalf("body marker: %q", r.Body)
	}
	flat := reviewOf(t, strings.Replace(results, `"head_sha":"h"`, `"head_sha":"h","inline":false`, 1))
	if !strings.HasSuffix(flat.Body, "<!-- agent-review v2 loose-blocking=2 -->") {
		t.Fatalf("flat body marker: %q", flat.Body)
	}
}

// threads turns the PR's review threads into open_threads: the reviewer's own,
// unresolved and not outdated, read from the marker or the first format, and
// blocking when unreadable.
func TestAgentReviewThreads(t *testing.T) {
	in := `{"bots":["hyperscale-reviewer[bot]","hyperscale-agent[bot]"],"threads":[
		{"id":"T1","isResolved":false,"isOutdated":false,"path":"x.go","line":5,"startLine":3,"comments":{"nodes":[{"databaseId":11,"url":"https://github.com/o/r/pull/1#discussion_r11","author":{"login":"hyperscale-reviewer"},
			"body":"**❌ Use a real db**\n\n<details>x</details>\n\n<!-- agent-review severity=blocking lens=catalog-1 rule=mocked-sut -->"}]}},
		{"isResolved":false,"isOutdated":false,"path":"a.sh","line":9,"comments":{"nodes":[{"databaseId":12,"author":{"login":"hyperscale-agent"},
			"body":"**[advisory] trust-bypass via dedup** (lens invariants)\n\nFailure: x"}]}},
		{"isResolved":false,"isOutdated":false,"path":"b.go","line":2,"comments":{"nodes":[{"author":{"login":"hyperscale-reviewer"},"body":"something else"}]}},
		{"isResolved":true,"isOutdated":false,"path":"c.go","line":1,"comments":{"nodes":[{"author":{"login":"hyperscale-reviewer"},"body":"**❌ t**"}]}},
		{"isResolved":false,"isOutdated":true,"path":"d.go","line":null,"comments":{"nodes":[{"author":{"login":"hyperscale-reviewer"},"body":"**❌ t**"}]}},
		{"isResolved":false,"isOutdated":true,"path":"f.go","line":null,"originalLine":8,"originalStartLine":6,"comments":{"nodes":[{"author":{"login":"hyperscale-reviewer"},"body":"**❌ Moved on**"}]}},
		{"isResolved":false,"isOutdated":false,"path":"e.go","line":1,"comments":{"nodes":[{"author":{"login":"fullmetal-fred"},"body":"**❌ t**"}]}}
	]}`
	out, code := agentReview(t, in, "threads")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if len(got) != 4 {
		t.Fatalf("got %d threads, want 4 (resolved, lineless and a person's are left out): %s", len(got), out)
	}
	want := []string{
		`x.go 5 3 mocked-sut blocking catalog-1 Use a real db`,
		`a.sh 9 9 trust-bypass-via-dedup advisory invariants trust-bypass via dedup`,
		`b.go 2 2  blocking  open finding`,
		`f.go 8 6  blocking  Moved on`,
	}
	for i, w := range want {
		g := got[i]
		line := strings.Join([]string{g["path"].(string), strconv.Itoa(int(g["line"].(float64))), strconv.Itoa(int(g["start_line"].(float64))),
			g["rule"].(string), g["severity"].(string), g["lens"].(string), g["title"].(string)}, " ")
		if line != w {
			t.Errorf("thread %d = %q, want %q", i, line, w)
		}
	}
	// An outdated thread stays open at its original lines, for the recheck lens.
	if got[0]["outdated"] != false || got[3]["outdated"] != true {
		t.Fatalf("outdated: %v %v", got[0]["outdated"], got[3]["outdated"])
	}
	if got[0]["thread_id"] != "T1" || got[0]["comment_id"] != float64(11) {
		t.Fatalf("thread ids: %v", got[0])
	}
	if d := got[0]["detail"].(string); strings.Contains(d, "<!--") || strings.Contains(d, "<details>") || !strings.Contains(d, "Use a real db") {
		t.Fatalf("detail = %q, want the comment without the marker or tags", d)
	}
	if _, code := agentReview(t, `{"threads":[]}`, "threads"); code != 2 {
		t.Fatalf("no bots: exit %d, want 2", code)
	}
}

// TestAgentReviewTurns checks that the turn limit scales with diff size so
// small PRs stop sooner and very large ones get more room.
func TestAgentReviewTurns(t *testing.T) {
	for _, c := range []struct {
		lines int
		want  int
	}{
		{0, 40},
		{100, 40},
		{101, 60},
		{500, 60},
		{501, 80},
		{2000, 80},
		{2001, 120},
	} {
		out, code := agentReview(t, "", "turns", strconv.Itoa(c.lines))
		if code != 0 {
			t.Fatalf("turns %d: exit %d", c.lines, code)
		}
		if out != strconv.Itoa(c.want) {
			t.Errorf("turns %d: got %s, want %d", c.lines, out, c.want)
		}
	}
	if _, code := agentReview(t, "", "turns", "notanumber"); code != 2 {
		t.Fatalf("non-integer: exit %d, want 2", code)
	}
	if _, code := agentReview(t, "", "turns"); code != 64 {
		t.Fatalf("missing arg: exit %d, want 64", code)
	}
}

// TestAgentReviewIncrementalBase checks that the reviewer's last review
// anchors a re-review only when it is in the v2 format with no blocking finding
// left in its body.
func TestAgentReviewIncrementalBase(t *testing.T) {
	const sha1 = "1111111111111111111111111111111111111111"
	const sha2 = "2222222222222222222222222222222222222222"
	ok := "❌ Changes requested · 1 blocking · 0 advisory <!-- agent-review v2 loose-blocking=0 -->"
	base := func(reviews string) string {
		t.Helper()
		got, code := agentReview(t, `{"reviewer_bot":"Reviewer[Bot]","reviews":[`+reviews+`]}`, "incremental-base")
		if code != 0 {
			t.Fatalf("exit %d: %s", code, got)
		}
		return got
	}
	if got := base(`{"user":"reviewer[bot]","commit_id":"` + sha1 + `","body":"` + ok + `"},
		{"user":"someone","commit_id":"` + sha1 + `","body":""},
		{"user":"reviewer[bot]","commit_id":"` + sha2 + `","body":"` + ok + `"}`); got != sha2 {
		t.Fatalf("got %q, want the last reviewed head", got)
	}
	for name, reviews := range map[string]string{
		"no review yet":          `{"user":"other","commit_id":"` + sha1 + `","body":"` + ok + `"}`,
		"none at all":            ``,
		"a pre-v2 review":        `{"user":"reviewer[bot]","commit_id":"` + sha1 + `","body":"agent-review: 0 blocking"}`,
		"a blocker in the body":  `{"user":"reviewer[bot]","commit_id":"` + sha1 + `","body":"<!-- agent-review v2 loose-blocking=1 -->"}`,
		"a commit id that isn't": `{"user":"reviewer[bot]","commit_id":"main","body":"` + ok + `"}`,
	} {
		if got := base(reviews); got != "" {
			t.Errorf("%s: got %q, want a full review", name, got)
		}
	}
	if _, code := agentReview(t, `[]`, "incremental-base"); code != 2 {
		t.Fatalf("array input: exit %d, want 2", code)
	}
	if _, code := agentReview(t, "", "incremental-base", "extra"); code != 64 {
		t.Fatalf("extra arg: exit %d, want 64", code)
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commitFile(t *testing.T, dir, name, body, msg string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", name)
	git(t, dir, "commit", "-q", "-m", msg)
	return git(t, dir, "rev-parse", "HEAD")
}

func delta(t *testing.T, dir string, args ...string) (string, string) {
	t.Helper()
	requireTools(t)
	out := filepath.Join(t.TempDir(), "review.diff")
	script, _ := filepath.Abs("agent-review.sh")
	cmd := exec.Command("bash", append([]string{script, "delta", out}, args...)...)
	cmd.Dir = dir
	said, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("delta %v: %v: %s", args, err, said)
	}
	d, _ := os.ReadFile(out)
	return strings.TrimSpace(string(said)), string(d)
}

// TestAgentReviewDelta builds a PR in a real repository: a re-review reads the
// PR's own commits since the last reviewed head, not what a merge from main
// brought in, and anything it cannot anchor falls back to the full diff.
func TestAgentReviewDelta(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	commitFile(t, dir, "main.txt", "one\n", "base")
	git(t, dir, "checkout", "-q", "-b", "pr")
	commitFile(t, dir, "pr.go", "package x\n", "pr: first")
	reviewed := git(t, dir, "rev-parse", "HEAD")

	// main moves on, the PR merges it, then pushes one more change.
	git(t, dir, "checkout", "-q", "main")
	commitFile(t, dir, "main.txt", "one\ntwo\n", "main: second")
	git(t, dir, "checkout", "-q", "pr")
	git(t, dir, "merge", "-q", "--no-edit", "main")
	commitFile(t, dir, "pr.go", "package x\n\nvar Y = 1\n", "pr: second")

	said, d := delta(t, dir, "main", reviewed)
	if said != "incremental "+reviewed {
		t.Fatalf("said %q", said)
	}
	if !strings.Contains(d, "+var Y = 1") || strings.Contains(d, "main.txt") || strings.Contains(d, "+package x") {
		t.Fatalf("delta should hold only the PR's second change:\n%s", d)
	}

	// No earlier review, or nothing pushed since: the full PR diff.
	for _, last := range []string{"", git(t, dir, "rev-parse", "HEAD")} {
		args := []string{"main"}
		if last != "" {
			args = append(args, last)
		}
		said, d := delta(t, dir, args...)
		if !strings.HasPrefix(said, "full: ") || !strings.Contains(d, "+package x") || strings.Contains(d, "main.txt") {
			t.Fatalf("last %q: said %q, diff:\n%s", last, said, d)
		}
	}

	// A force push that drops the reviewed head: the full PR diff.
	git(t, dir, "checkout", "-q", "-b", "rewritten", "main")
	commitFile(t, dir, "pr.go", "package z\n", "pr: rewritten")
	if said, d := delta(t, dir, "main", reviewed); !strings.HasPrefix(said, "full: ") || !strings.Contains(d, "+package z") {
		t.Fatalf("rewritten: said %q, diff:\n%s", said, d)
	}
	// A reviewed head that is not in the repository at all.
	if said, _ := delta(t, dir, "main", strings.Repeat("ab", 20)); !strings.HasPrefix(said, "full: ") {
		t.Fatalf("missing head: said %q", said)
	}

	// A merge from main that changes nothing of the PR's own: an empty delta.
	git(t, dir, "checkout", "-q", "pr")
	head := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "checkout", "-q", "main")
	commitFile(t, dir, "main.txt", "one\ntwo\nthree\n", "main: third")
	git(t, dir, "checkout", "-q", "pr")
	git(t, dir, "merge", "-q", "--no-edit", "main")
	if said, d := delta(t, dir, "main", head); said != "incremental "+head || strings.TrimSpace(d) != "" {
		t.Fatalf("main-only merge: said %q, diff:\n%s", said, d)
	}
}

// Every re-review with open threads gets a recheck lens that lists each one,
// so each thread is judged on its own and not only when a lens trips over it.
func TestAgentReviewLensesAddARecheckForOpenThreads(t *testing.T) {
	dir := catalog(t, "a-entry.md")
	open := filepath.Join(t.TempDir(), "open.json")
	if err := os.WriteFile(open, []byte(`[{"comment_id":11,"path":"x.go","line":5,"severity":"blocking","title":"Use a real db","detail":"Failure: mocks the store"},
		{"comment_id":12,"path":"y.go","line":3,"severity":"advisory","title":"t","outdated":true}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"", "+++ b/docs/page.md\n", "+++ b/x.go\n"} {
		f := filepath.Join(t.TempDir(), "x.diff")
		if err := os.WriteFile(f, []byte(d), 0o644); err != nil {
			t.Fatal(err)
		}
		out, code := agentReview(t, "", "lenses", dir, f, open)
		if code != 0 {
			t.Fatalf("%q: exit %d: %s", d, code, out)
		}
		var got []lens
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		last := got[len(got)-1]
		if last.Name != "recheck" || !strings.Contains(last.Task, "comment_id 11: blocking at `x.go:5`, Use a real db. Failure: mocks the store") {
			t.Fatalf("%q: last lens %+v, want the recheck lens listing comment 11", d, last)
		}
		if !strings.Contains(last.Task, "comment_id 12: advisory at `y.go:3` (that line is from the version it was raised on") {
			t.Fatalf("%q: the recheck lens is not told comment 12 is outdated: %s", d, last.Task)
		}
	}
	empty := filepath.Join(t.TempDir(), "none.json")
	if err := os.WriteFile(empty, []byte(`[]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, _ := agentReview(t, "", "lenses", dir, "", empty); strings.Contains(out, "recheck") {
		t.Fatalf("no open threads should mean no recheck lens: %s", out)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte(`not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, code := agentReview(t, "", "lenses", dir, "", bad); code != 2 {
		t.Fatalf("bad open threads: exit %d, want 2", code)
	}
}

const recheckResults = `{"head_sha":"abcdef1234","expected":["a","recheck"],
	"open_threads":[
		{"comment_id":11,"thread_id":"T11","path":"x.go","line":5,"rule":"r","severity":"blocking","lens":"a","title":"Fixed one"},
		{"comment_id":12,"thread_id":"T12","path":"y.go","line":7,"rule":"s","severity":"blocking","lens":"a","title":"Still broken"},
		{"comment_id":13,"thread_id":"T13","path":"z.go","line":9,"rule":"q","severity":"blocking","lens":"a","title":"Said fixed, found again"},
		{"comment_id":14,"thread_id":"T14","path":"w.go","line":2,"rule":"p","severity":"advisory","lens":"a","title":"No word"}],
	"lenses":[
		{"name":"a","findings":[{"severity":"blocking","path":"z.go","line":9,"rule":"q","failure":"f","fix":"x"}]},
		{"name":"recheck","summary":"checked 3","findings":[],"rechecks":[
			{"comment_id":11,"status":"fixed","note":"Uses the test db now."},
			{"comment_id":12,"status":"open","note":"Still mocks the store."},
			{"comment_id":13,"status":"fixed","note":"Looks fixed."},
			{"comment_id":99,"status":"fixed","note":"Not a thread."}]}]}`

// A thread stops counting only when the recheck lens says it is fixed and no
// lens raised it again; one with no word from the recheck lens still counts.
func TestAgentReviewAppliesTheRecheck(t *testing.T) {
	r := reviewOf(t, recheckResults)
	if r.Event != "REQUEST_CHANGES" || !strings.HasPrefix(r.Body, "❌ Changes requested · 2 blocking · 1 advisory") {
		t.Fatalf("%s %q, want 12 and 13 blocking and 14 advisory", r.Event, r.Body)
	}
	if !strings.Contains(r.Body, "Fixed since the last review:\n✅ `x.go:5` Fixed one") {
		t.Fatalf("body does not list the fixed thread: %q", r.Body)
	}
	for _, still := range []string{"↪ ❌ `y.go:7` Still broken", "↪ ❌ `z.go:9` Said fixed, found again", "↪ ⚠️ `w.go:2` No word"} {
		if !strings.Contains(r.Body, still) {
			t.Fatalf("body does not carry %q: %q", still, r.Body)
		}
	}
	if strings.Contains(r.Body, "↪ ❌ `x.go:5`") {
		t.Fatalf("the fixed thread is still carried: %q", r.Body)
	}
	if len(r.Comments) != 0 {
		t.Fatalf("comments %+v: z.go:9 is already on a thread", r.Comments)
	}
	if !strings.Contains(r.Body, "✅ a · ❌ recheck") {
		t.Fatalf("lens strip should put the threads still open on recheck: %q", r.Body)
	}

	// Every thread fixed: the review ships.
	ok := reviewOf(t, `{"head_sha":"h","expected":["a","recheck"],
		"open_threads":[{"comment_id":11,"path":"x.go","line":5,"rule":"r","severity":"blocking","title":"t"}],
		"lenses":[{"name":"a","findings":[]},{"name":"recheck","findings":[],"rechecks":[{"comment_id":11,"status":"fixed","note":"n"}]}]}`)
	if ok.Event != "COMMENT" || !strings.HasPrefix(ok.Body, "✅ Ship · nothing found") {
		t.Fatalf("%s %q", ok.Event, ok.Body)
	}
	// Another lens cannot clear a thread.
	other := reviewOf(t, `{"head_sha":"h","expected":["a"],
		"open_threads":[{"comment_id":11,"path":"x.go","line":5,"rule":"r","severity":"blocking","title":"t"}],
		"lenses":[{"name":"a","findings":[],"rechecks":[{"comment_id":11,"status":"fixed","note":"n"}]}]}`)
	if other.Event != "REQUEST_CHANGES" {
		t.Fatalf("a recheck from lens a cleared the thread: %q", other.Body)
	}
}

// followups replies on each thread the recheck lens judged, and resolves only
// the ones that are fixed.
func TestAgentReviewFollowups(t *testing.T) {
	out, code := agentReview(t, recheckResults, "followups")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	var got []struct {
		CommentID int    `json:"comment_id"`
		ThreadID  string `json:"thread_id"`
		Resolve   bool   `json:"resolve"`
		Body      string `json:"body"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	want := []struct {
		id      int
		resolve bool
		body    string
	}{
		{11, true, "✅ Verified fixed at abcdef1. Uses the test db now."},
		{12, false, "❌ Still open at abcdef1. Still mocks the store."},
		{13, false, "❌ Still open at abcdef1: a lens raised it again. Looks fixed."},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d follow-ups, want %d (no word on 14, 99 is no thread): %s", len(got), len(want), out)
	}
	for i, w := range want {
		g := got[i]
		if g.CommentID != w.id || g.Resolve != w.resolve || g.Body != w.body || g.ThreadID != "T"+strconv.Itoa(w.id) {
			t.Errorf("follow-up %d = %+v, want %+v", i, g, w)
		}
	}
	// A fixed thread without a thread id is replied to but not resolved.
	out2, _ := agentReview(t, strings.Replace(recheckResults, `"thread_id":"T11",`, ``, 1), "followups")
	if !strings.Contains(out2, `"comment_id":11,"thread_id":"","resolve":false`) {
		t.Fatalf("no thread id: %s", out2)
	}
}
