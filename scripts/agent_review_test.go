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

// The one-shot reviewer (#2250): a script builds its whole prompt, one model
// call reviews it, and only a blocking finding it is unsure of goes to a
// short verify session.

// prRepo builds a checkout of a PR head that carries the skill, its catalog,
// the solidjs skill and a docs page, with a PR commit on top of main. It
// returns the repo, the full diff, and the main commit.
func prRepo(t *testing.T, prFiles map[string]string) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, dir, "init", "-q", "-b", "main")
	write(".claude/skills/adversarial-review/SKILL.md", "# Adversarial review\nREFUTE-BOTH-WAYS\n")
	write(".claude/skills/adversarial-review/references/anti-patterns/README.md", "the catalog's readme\n")
	write(".claude/skills/adversarial-review/references/anti-patterns/alpha-cue.md", "## alpha-cue (live)\nALPHA-ENTRY\n")
	write(".claude/skills/adversarial-review/references/anti-patterns/beta-cue.md", "## beta-cue (live)\nBETA-ENTRY\n")
	write(".claude/skills/solidjs/SKILL.md", "SOLID-SKILL\n")
	write("docs/guide.md", "Call `OldRetryLimit` to cap retries.\nNothing else.\n")
	write("internal/x.go", "package x\n\nconst OldRetryLimit = 3\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "base")
	base := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "checkout", "-q", "-b", "pr")
	for name, body := range prFiles {
		write(name, body)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "pr")
	diff := filepath.Join(t.TempDir(), "review.diff")
	if err := os.WriteFile(diff, []byte(git(t, dir, "diff", base+"...HEAD")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, diff, base
}

func buildContext(t *testing.T, dir string, args ...string) (string, string) {
	t.Helper()
	requireTools(t)
	out := t.TempDir()
	script, _ := filepath.Abs("agent-review.sh")
	cmd := exec.Command("bash", append([]string{script, "context", out}, args...)...)
	cmd.Dir = dir
	said, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("context %v: %v: %s", args, err, said)
	}
	static, _ := os.ReadFile(filepath.Join(out, "static.md"))
	pr, _ := os.ReadFile(filepath.Join(out, "pr.md"))
	return string(static), string(pr)
}

func TestAgentReviewContextFirstReview(t *testing.T) {
	dir, diff, _ := prRepo(t, map[string]string{"internal/x.go": "package x\n\nconst NewRetryLimit = 5\n"})
	issue := filepath.Join(t.TempDir(), "issue.md")
	if err := os.WriteFile(issue, []byte("SCOPE-CONTRACT text"), 0o644); err != nil {
		t.Fatal(err)
	}
	static, pr := buildContext(t, dir, diff, "", issue)
	for _, want := range []string{"one-shot adversarial reviewer", "REFUTE-BOTH-WAYS", `<catalog-entry file="alpha-cue.md">`, "ALPHA-ENTRY", "BETA-ENTRY"} {
		if !strings.Contains(static, want) {
			t.Fatalf("static part lacks %q:\n%s", want, static)
		}
	}
	if strings.Contains(static, "the catalog's readme") || strings.Contains(static, "SOLID-SKILL") {
		t.Fatalf("static part has the readme or the solidjs skill for a Go-only diff:\n%s", static)
	}
	if strings.Index(static, "alpha-cue") > strings.Index(static, "beta-cue") {
		t.Fatal("catalog entries are not in a stable order")
	}
	for _, want := range []string{
		"SCOPE-CONTRACT text",
		"This is the whole diff",
		"+const NewRetryLimit = 5",
		`<file path="internal/x.go" lines="1-3">`,
		"     3  const NewRetryLimit = 5",
		// The neighbor check: the docs line naming what the diff removed.
		"docs/guide.md:1:Call `OldRetryLimit` to cap retries.",
	} {
		if !strings.Contains(pr, want) {
			t.Fatalf("PR part lacks %q:\n%s", want, pr)
		}
	}
	if strings.Contains(pr, "Open findings") {
		t.Fatalf("a first review lists open findings:\n%s", pr)
	}
	// The static part is the same for another PR, so it caches.
	dir2, diff2, _ := prRepo(t, map[string]string{"internal/y.go": "package x\n"})
	static2, _ := buildContext(t, dir2, diff2)
	if static2 != static {
		t.Fatal("the static part changed with the PR")
	}
}

func TestAgentReviewContextReReviewWithOpenThreads(t *testing.T) {
	body := "package x\n\n" + strings.Repeat("// filler\n", 20) + "const NewRetryLimit = 5\n"
	dir, diff, _ := prRepo(t, map[string]string{"internal/x.go": body})
	open := filepath.Join(t.TempDir(), "open.json")
	if err := os.WriteFile(open, []byte(`[{"comment_id":41,"path":"internal/x.go","line":23,"severity":"blocking","rule":"magic-number","title":"Bare limit","detail":"A bare 5."},
		{"comment_id":42,"path":"gone.go","line":3,"severity":"advisory","rule":"r","title":"Old","outdated":true}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	last := git(t, dir, "rev-parse", "HEAD~1")
	_, pr := buildContext(t, dir, diff, open, "", last)
	for _, want := range []string{
		"This is a re-review",
		last,
		"Open findings from earlier reviews (recheck each)",
		"comment_id 41: blocking magic-number at internal/x.go:23",
		"It said: Bare limit A bare 5.",
		`<file path="internal/x.go" lines="15-31">`,
		"    23  const NewRetryLimit = 5",
		"comment_id 42: advisory r at gone.go:3 (that line is from the version it was raised on",
		"The file is gone from the PR.",
	} {
		if !strings.Contains(pr, want) {
			t.Fatalf("PR part lacks %q:\n%s", want, pr)
		}
	}
	// A last head that is not a commit id is refused.
	out := t.TempDir()
	script, _ := filepath.Abs("agent-review.sh")
	cmd := exec.Command("bash", script, "context", out, diff, open, "", "not-a-sha")
	cmd.Dir = dir
	if err := cmd.Run(); err == nil {
		t.Fatal("context took a last head that is no commit id")
	}
}

func TestAgentReviewContextWebDiffAddsSolid(t *testing.T) {
	dir, diff, _ := prRepo(t, map[string]string{"web/src/pages/A.tsx": "export const A = () => <div />;\n"})
	static, pr := buildContext(t, dir, diff)
	if !strings.Contains(static, "SOLID-SKILL") {
		t.Fatalf("a diff under web/src lacks the solidjs skill:\n%s", static)
	}
	if !strings.Contains(pr, `<file path="web/src/pages/A.tsx" lines="1-1">`) {
		t.Fatalf("PR part lacks the web file:\n%s", pr)
	}
}

func TestAgentReviewContextLargeFileGoesInByHunk(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 1000; i++ {
		b.WriteString("line " + strconv.Itoa(i) + "\n")
	}
	dir, _, base := prRepo(t, map[string]string{"big.txt": b.String()})
	changed := strings.Replace(b.String(), "line 500\n", "line FIVE HUNDRED\n", 1)
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "commit", "-q", "-am", "change one line")
	diff := filepath.Join(t.TempDir(), "review.diff")
	if err := os.WriteFile(diff, []byte(git(t, dir, "diff", base+"...HEAD")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, pr := buildContext(t, dir, diff)
	// A new file of 1000 lines is one hunk of all of it; check the edit commit alone.
	diff2 := filepath.Join(t.TempDir(), "review2.diff")
	if err := os.WriteFile(diff2, []byte(git(t, dir, "diff", "HEAD~1", "HEAD")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, pr2 := buildContext(t, dir, diff2)
	if !strings.Contains(pr2, `<file path="big.txt" lines="457-544" of="1000">`) || strings.Contains(pr2, "  line 1\n") {
		t.Fatalf("a large file did not go in by hunk:\n%s", pr2)
	}
	if !strings.Contains(pr, `of="1000"`) {
		t.Fatalf("the whole-PR context lost the large file:\n%.2000s", pr)
	}
}

func TestAgentReviewToVerify(t *testing.T) {
	out, code := agentReview(t, `{"summary":"s","findings":[
		{"severity":"blocking","path":"a.go","line":1,"rule":"r","title":"sure","failure":"f","fix":"x","verify":false},
		{"severity":"blocking","path":"b.go","line":2,"rule":"r","title":"unsure","failure":"f","fix":"x","verify":true},
		{"severity":"advisory","path":"c.go","line":3,"rule":"r","title":"advisory","failure":"f","fix":"x","verify":true}]}`, "to-verify")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if out != `[{"severity":"blocking","path":"b.go","line":2,"rule":"r","title":"unsure","failure":"f","fix":"x","index":1}]` {
		t.Fatalf("to-verify = %s", out)
	}
	if out, _ := agentReview(t, `{"findings":[]}`, "to-verify"); out != "[]" {
		t.Fatalf("nothing to verify = %s", out)
	}
	if _, code := agentReview(t, `[]`, "to-verify"); code != 2 {
		t.Fatalf("bad input: exit %d, want 2", code)
	}
}

func TestAgentReviewOneshotResult(t *testing.T) {
	oneshot := `{"summary":"swept","findings":[
		{"severity":"blocking","path":"a.go","line":1,"rule":"r","title":"sure","failure":"f","fix":"x","verify":false},
		{"severity":"blocking","path":"b.go","line":2,"rule":"r","title":"refuted","failure":"f","fix":"x","verify":true},
		{"severity":"blocking","path":"c.go","line":3,"rule":"r","title":"no word","failure":"f","fix":"x","verify":true},
		{"severity":"blocking","path":"d.go","line":4,"rule":"r","title":"confirmed","failure":"f","fix":"x","verify":true}],
		"rechecks":[{"comment_id":7,"status":"fixed","note":"n"}]}`
	out, code := agentReview(t, `{"oneshot":`+oneshot+`,"verify":{"results":[{"index":1,"status":"refuted"},{"index":3,"status":"confirmed"}]}}`, "oneshot-result")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	var got struct {
		Name     string `json:"name"`
		Summary  string `json:"summary"`
		Findings []map[string]any
		Rechecks []map[string]any
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	var titles []string
	for _, f := range got.Findings {
		titles = append(titles, f["title"].(string))
		if _, ok := f["verify"]; ok {
			t.Fatalf("verify left on a finding: %s", out)
		}
	}
	if got.Name != "oneshot" || strings.Join(titles, ",") != "sure,no word,confirmed" || len(got.Rechecks) != 1 {
		t.Fatalf("oneshot-result = %s", out)
	}
	if got.Summary != "swept (verify dropped 1)" {
		t.Fatalf("summary = %q", got.Summary)
	}
	// With no verify session, nothing is dropped.
	out2, _ := agentReview(t, `{"oneshot":`+oneshot+`,"verify":null}`, "oneshot-result")
	if strings.Count(out2, `"title"`) != 4 {
		t.Fatalf("no verify: %s", out2)
	}
	if _, code := agentReview(t, `{"verify":{}}`, "oneshot-result"); code != 2 {
		t.Fatalf("bad input: exit %d, want 2", code)
	}
}

// The one-shot reviewer's own recheck counts, like the recheck lens's, and a
// finding it raised that matches a thread it judged fixed never blocks (PR
// #2225, review 5985358926).
func TestAgentReviewOneshotRecheckNeverReraisesWhatItFixed(t *testing.T) {
	r := reviewOf(t, `{"head_sha":"h","expected":["oneshot"],
		"open_threads":[{"comment_id":11,"path":"x.go","line":5,"rule":"mocked-sut","severity":"blocking","title":"Mocks the store"}],
		"lenses":[{"name":"oneshot","summary":"s",
			"findings":[{"severity":"blocking","path":"x.go","line":5,"rule":"mocked-sut","title":"Mocks the store","failure":"f","fix":"x"}],
			"rechecks":[{"comment_id":11,"status":"fixed","note":"Uses the test db now."}]}]}`)
	if r.Event != "COMMENT" || !strings.HasPrefix(r.Body, "✅ Ship · nothing found") {
		t.Fatalf("%s %q: the finding it judged fixed still blocks", r.Event, r.Body)
	}
	if !strings.Contains(r.Body, "Fixed since the last review:\n✅ `x.go:5` Mocks the store") {
		t.Fatalf("body = %q", r.Body)
	}
	// Another lens raising it again still keeps the thread open.
	again := reviewOf(t, `{"head_sha":"h","expected":["oneshot","neighbor"],
		"open_threads":[{"comment_id":11,"path":"x.go","line":5,"rule":"mocked-sut","severity":"blocking","title":"Mocks the store"}],
		"lenses":[{"name":"oneshot","findings":[],"rechecks":[{"comment_id":11,"status":"fixed","note":"n"}]},
			{"name":"neighbor","findings":[{"severity":"blocking","path":"x.go","line":5,"rule":"mocked-sut","failure":"f","fix":"x"}]}]}`)
	if again.Event != "REQUEST_CHANGES" {
		t.Fatalf("another lens's finding was dropped: %q", again.Body)
	}
	// An open recheck from the one-shot reviewer keeps the blocker.
	open := reviewOf(t, `{"head_sha":"h","expected":["oneshot"],
		"open_threads":[{"comment_id":11,"path":"x.go","line":5,"rule":"mocked-sut","severity":"blocking","title":"Mocks the store"}],
		"lenses":[{"name":"oneshot","findings":[],"rechecks":[{"comment_id":11,"status":"open","note":"n"}]}]}`)
	if open.Event != "REQUEST_CHANGES" || !strings.Contains(open.Body, "↪ ❌ `x.go:5` Mocks the store") {
		t.Fatalf("%s %q", open.Event, open.Body)
	}
}

func TestAgentReviewGate(t *testing.T) {
	ignore := `^(plan|agent-review|oneshot|lens \\(.*\\))$`
	for _, c := range []struct {
		name, in, want string
	}{
		{"green", `{"mergeable":"MERGEABLE","checks":[{"name":"test","status":"completed","conclusion":"success"},{"name":"lint","status":"completed","conclusion":"skipped"}]}`, "review"},
		{"conflict", `{"mergeable":"CONFLICTING","checks":[]}`, "hold: the PR conflicts with its base"},
		{"red", `{"mergeable":"MERGEABLE","checks":[{"name":"test","status":"completed","conclusion":"failure"},{"name":"e2e","status":"in_progress"},{"name":"test","status":"completed","conclusion":"failure"}]}`, "hold: test failed"},
		{"running", `{"mergeable":"UNKNOWN","checks":[{"name":"test","status":"in_progress"},{"name":"lint","status":"completed","conclusion":"success"}]}`, "wait"},
		{"own checks ignored", `{"mergeable":"MERGEABLE","checks":[{"name":"agent-review","status":"completed","conclusion":"failure"},{"name":"lens (invariants)","status":"completed","conclusion":"failure"},{"name":"plan","status":"in_progress"}]}`, "review"},
		// The tests gate the review (#2597): a required check that has not
		// registered yet is not a green PR.
		{"required check not registered yet", `{"require":["test","e2e"],"mergeable":"MERGEABLE","checks":[{"name":"changes","status":"completed","conclusion":"success"},{"name":"test","status":"queued"}]}`, "wait"},
		{"no checks at all yet", `{"require":["test"],"mergeable":"MERGEABLE","checks":[]}`, "wait"},
		{"every required check done", `{"require":["test","e2e"],"mergeable":"MERGEABLE","checks":[{"name":"test","status":"completed","conclusion":"success"},{"name":"e2e","status":"completed","conclusion":"success"}]}`, "review"},
		{"a skipped required check counts as done", `{"require":["e2e"],"mergeable":"MERGEABLE","checks":[{"name":"e2e","status":"completed","conclusion":"skipped"}]}`, "review"},
		{"red wins over a missing check", `{"require":["test","e2e"],"mergeable":"MERGEABLE","checks":[{"name":"test","status":"completed","conclusion":"failure"}]}`, "hold: test failed"},
		// Only the newest run of a name counts (#2629): a superseded or
		// re-run check leaves its older runs on the head.
		{"an older red run is superseded by a newer green one", `{"require":["test"],"mergeable":"MERGEABLE","checks":[{"id":1,"name":"test","status":"completed","conclusion":"failure"},{"id":2,"name":"test","status":"completed","conclusion":"success"}]}`, "review"},
		{"the newest run wins whatever order it is listed in", `{"require":["test"],"mergeable":"MERGEABLE","checks":[{"id":2,"name":"test","status":"completed","conclusion":"success"},{"id":1,"name":"test","status":"completed","conclusion":"cancelled"}]}`, "review"},
		{"an older green run does not hide a newer red one", `{"require":["e2e"],"mergeable":"MERGEABLE","checks":[{"id":1,"name":"e2e","status":"completed","conclusion":"success"},{"id":2,"name":"e2e","status":"completed","conclusion":"failure"}]}`, "hold: e2e failed"},
	} {
		in := strings.Replace(c.in, `{"mergeable"`, `{"ignore":"`+ignore+`","mergeable"`, 1)
		out, code := agentReview(t, in, "gate")
		if code != 0 || out != c.want {
			t.Errorf("%s: %q (exit %d), want %q", c.name, out, code, c.want)
		}
	}
	if _, code := agentReview(t, `{}`, "gate"); code != 2 {
		t.Fatalf("bad input: exit %d, want 2", code)
	}
}

// The reviewer on its threads (#2253).

func TestAgentReviewReplyGate(t *testing.T) {
	base := `{"sender":"fred","sender_type":"User","in_reply_to_id":5,"root_author":"hyperscale-reviewer","bots":["hyperscale-reviewer[bot]","hyperscale-agent[bot]"]}`
	cases := map[string]string{
		"run":                     base,
		"skip: a top-level":       strings.Replace(base, `"in_reply_to_id":5`, `"in_reply_to_id":null`, 1),
		"skip: the reply is from": strings.Replace(base, `"User"`, `"Bot"`, 1),
		"skip: the thread is not": strings.Replace(base, `"root_author":"hyperscale-reviewer"`, `"root_author":"fred"`, 1),
	}
	for want, in := range cases {
		out, code := agentReview(t, in, "reply-gate")
		if code != 0 || !strings.HasPrefix(out, want) {
			t.Errorf("%s: got %q (%d)", want, out, code)
		}
	}
	if _, code := agentReview(t, "[]", "reply-gate"); code != 2 {
		t.Errorf("bad input exit %d", code)
	}
}

func TestAgentReviewReplyPromptCarriesOnlyTheThread(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "mocked-sut.md"), []byte("ENTRY-TEXT"), 0o644)
	in := `{"path":"x.go","line":7,"lines":"LINE-SEVEN","comments":[
		{"author":"hyperscale-reviewer","body":"**❌ t**\n<!-- agent-review severity=blocking lens=a rule=mocked-sut -->"},
		{"author":"fred","body":"it is fine"}]}`
	out, code := agentReview(t, in, "reply-prompt", dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	for _, w := range []string{"x.go:7", "LINE-SEVEN", "ENTRY-TEXT", "it is fine"} {
		if !strings.Contains(out, w) {
			t.Errorf("prompt lacks %q", w)
		}
	}
}

func TestAgentReviewReplyResult(t *testing.T) {
	for in, want := range map[string]string{
		`{"resolve":true,"reply":"Agreed — fine."}`:                 `{"body":"Agreed - fine.","resolve":true}`,
		"Here:\n```json\n{\"resolve\":false,\"reply\":\"no\"}\n```": `{"body":"no","resolve":false}`,
		`garbage`: `{"body":"","resolve":false}`,
	} {
		if out, _ := agentReview(t, in, "reply-result"); out != want {
			t.Errorf("%q: got %s want %s", in, out, want)
		}
	}
}

// A thread the reviewer verified fixed is resolved in the same run, an
// advisory one included; the thread id is found from the comment when the
// open-thread list has none.
// A head the reviewer has already reviewed costs no second review (#2277): a
// reopen, a relabel, a draft flipped to ready or a manual run on a head that
// has a completed review by an App bot with the round marker says "seen". A new
// commit, a forced run and the review:deep label always review.
func TestAgentReviewSeenSkipsAReviewedHead(t *testing.T) {
	const head = "abcdef1234567890abcdef1234567890abcdef12"
	marker := `ok\n<!-- agent-review round blocking=0 head=abcdef1234567890abcdef1234567890abcdef12 -->`
	rev := func(id int, user, typ, state, commit, body string) string {
		return `{"id":` + strconv.Itoa(id) + `,"user":"` + user + `","type":"` + typ + `","state":"` + state + `","commit_id":"` + commit + `","body":"` + body + `"}`
	}
	seen := func(event, action, label string, force bool, reviews ...string) string {
		in := `{"head_sha":"` + head + `","event":"` + event + `","action":"` + action + `","label":"` + label +
			`","force":` + strconv.FormatBool(force) + `,"reviews":[` + strings.Join(reviews, ",") + `]}`
		out, code := agentReview(t, in, "seen")
		if code != 0 {
			t.Fatalf("exit %d: %s", code, out)
		}
		return out
	}
	done := rev(7, "reviewer[bot]", "Bot", "COMMENTED", head, marker)
	cases := []struct {
		name, want string
		got        string
	}{
		{"reopened on a reviewed head", "seen: 7", seen("pull_request", "reopened", "", false, done)},
		{"ready_for_review on a reviewed head", "seen: 7", seen("pull_request", "ready_for_review", "", false, done)},
		{"a changes-requested review counts", "seen: 8", seen("pull_request", "reopened", "", false, rev(8, "reviewer[bot]", "Bot", "CHANGES_REQUESTED", head, marker))},
		{"the newest of several is named", "seen: 9", seen("pull_request", "reopened", "", false, done, rev(9, "agent[bot]", "Bot", "COMMENTED", head, marker))},
		{"a manual run on a reviewed head", "seen: 7", seen("workflow_dispatch", "", "", false, done)},
		{"a push always reviews", "review", seen("pull_request", "synchronize", "", false, done)},
		{"a forced manual run reviews", "review", seen("workflow_dispatch", "", "", true, done)},
		{"review:deep always reviews", "review", seen("pull_request", "labeled", "review:deep", false, done)},
		{"another head has no review of this one", "review", seen("pull_request", "reopened", "", false, rev(7, "reviewer[bot]", "Bot", "COMMENTED", "1111111111111111111111111111111111111111", marker))},
		{"a pending review is not complete", "review", seen("pull_request", "reopened", "", false, rev(7, "reviewer[bot]", "Bot", "PENDING", head, marker))},
		{"a dismissed review is not", "review", seen("pull_request", "reopened", "", false, rev(7, "reviewer[bot]", "Bot", "DISMISSED", head, marker))},
		{"a person's review is not the reviewer's", "review", seen("pull_request", "reopened", "", false, rev(7, "fred", "User", "COMMENTED", head, marker))},
		{"a bot review with no round marker is not the reviewer's", "review", seen("pull_request", "reopened", "", false, rev(7, "dependabot[bot]", "Bot", "COMMENTED", head, "bump"))},
		{"no reviews", "review", seen("pull_request", "reopened", "", false)},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, c.got, c.want)
		}
	}
	if _, code := agentReview(t, `{"reviews":[]}`, "seen"); code != 2 {
		t.Fatalf("input with no head: exit %d, want 2", code)
	}
}

func TestAgentReviewVerifiedFixedAdvisoryThreadResolves(t *testing.T) {
	r := `{"head_sha":"abcdef1234","expected":["recheck"],
		"open_threads":[{"path":"w.yml","line":135,"rule":"r","severity":"advisory","comment_id":21,"thread_id":"T21"}],
		"lenses":[{"name":"recheck","findings":[],"rechecks":[{"comment_id":21,"status":"fixed","note":""}]}]}`
	out, _ := agentReview(t, r, "followups")
	if !strings.Contains(out, `"resolve":true`) {
		t.Fatalf("advisory fixed thread not resolved: %s", out)
	}
	threads := `{"threads":[{"id":"TX","comments":{"nodes":[{"databaseId":4}]}},{"id":"TY","comments":{"nodes":[{"databaseId":21},{"databaseId":22}]}}]}`
	if out, _ := agentReview(t, threads, "thread-of", "22"); out != "TY" {
		t.Fatalf("thread-of = %q", out)
	}
	if out, _ := agentReview(t, threads, "thread-of", "9"); out != "" {
		t.Fatalf("thread-of unknown = %q", out)
	}
}

func TestAgentReviewReviewOpensWithHeadAndCarriesRoundMarker(t *testing.T) {
	r := reviewOf(t, `{"head_sha":"abcdef1234","expected":["a"],"lenses":[{"name":"a","findings":[{"severity":"blocking","path":"x.go","line":1,"rule":"r","failure":"f","fix":"x"}]}]}`)
	if !strings.HasPrefix(r.Body, "❌ Changes requested · 1 blocking · 0 advisory · head `abcdef1`") || !strings.Contains(r.Body, "<!-- agent-review round blocking=1 head=abcdef1234 -->") {
		t.Fatalf("body: %q", r.Body)
	}
	// The verdict still reads the blocking count under the head line.
	out, _ := agentTrust(t, "verdict", `{"app_bot":"b[bot]","head_sha":"h","reviews":[{"user":"b[bot]","state":"COMMENTED","commit_id":"h","body":`+strconv.Quote(r.Body)+`}]}`)
	if !strings.HasPrefix(out, "block") {
		t.Fatalf("verdict = %q", out)
	}
}

func TestAgentReviewRoundsSurviveDismissal(t *testing.T) {
	reviews := `[
	 {"user":"hyperscale-reviewer[bot]","state":"DISMISSED","body":"x\n<!-- agent-review round blocking=2 head=a -->"},
	 {"user":"hyperscale-reviewer[bot]","state":"DISMISSED","body":"Superseded by review 9."},
	 {"user":"hyperscale-reviewer[bot]","state":"COMMENTED","body":"clean\n<!-- agent-review round blocking=0 head=b -->"},
	 {"user":"hyperscale-reviewer[bot]","state":"CHANGES_REQUESTED","body":"old, no marker"},
	 {"user":"fred","state":"CHANGES_REQUESTED","body":"<!-- agent-review round blocking=3 -->"}]`
	if out, _ := agentReview(t, reviews, "rounds", "hyperscale-reviewer[bot],hyperscale-agent[bot]"); out != "2" {
		t.Fatalf("rounds = %q, want 2", out)
	}
}

func TestAgentReviewSupersede(t *testing.T) {
	reviews := `[
	 {"id":1,"node_id":"N1","user":"hyperscale-reviewer[bot]","state":"CHANGES_REQUESTED"},
	 {"id":2,"node_id":"N2","user":"hyperscale-reviewer[bot]","state":"COMMENTED"},
	 {"id":3,"node_id":"N3","user":"hyperscale-reviewer[bot]","state":"COMMENTED"},
	 {"id":4,"node_id":"N4","user":"fred","state":"CHANGES_REQUESTED"}]`
	out, _ := agentReview(t, reviews, "supersede", "hyperscale-reviewer[bot]", "3")
	var got []struct {
		ID      int  `json:"id"`
		Dismiss bool `json:"dismiss"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || len(got) != 2 || got[0].ID != 1 || !got[0].Dismiss || got[1].ID != 2 || got[1].Dismiss {
		t.Fatalf("supersede = %s (%v)", out, err)
	}
}

// agent-fix's own check is the fixer's, not the PR's: its failure must not hold
// the review (#2629). The ignore pattern is the workflow's own, so the two
// cannot drift.
func TestAgentReviewGateIgnoresAgentFixWithTheWorkflowsPattern(t *testing.T) {
	wf, err := os.ReadFile("../.github/workflows/agent-review.yml")
	if err != nil {
		t.Fatal(err)
	}
	var ignore string
	for line := range strings.SplitSeq(string(wf), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "IGNORE: '"); ok {
			ignore = strings.TrimSuffix(rest, "'")
		}
	}
	if ignore == "" {
		t.Fatal("no IGNORE in agent-review.yml")
	}
	facts, err := json.Marshal(map[string]any{
		"ignore":    ignore,
		"mergeable": "MERGEABLE",
		"require":   []string{"test"},
		"checks": []map[string]any{
			{"id": 1, "name": "test", "status": "completed", "conclusion": "success"},
			{"id": 2, "name": "agent-fix", "status": "completed", "conclusion": "failure"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out, code := agentReview(t, string(facts), "gate"); code != 0 || out != "review" {
		t.Fatalf("an agent-fix failure held the review: %q (exit %d), want review", out, code)
	}
}

// The Supersede step probes whether the default branch's agent-review.sh knows
// `supersede` (#2634). Actions runs a step's script with bash -e, and the probe's
// usage exit (64, by design) used to abort the step before it was read, so every
// review ended red. This runs the step's own probe lines under bash -e against a
// script that has supersede and one that does not.
func TestAgentReviewSupersedeProbeSurvivesErrexit(t *testing.T) {
	wf, err := os.ReadFile("../.github/workflows/agent-review.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(wf)
	start := strings.Index(text, "          set -uo pipefail\n          script=trusted/scripts/agent-review.sh\n")
	end := strings.Index(text[start+1:], "          reviews=$(gh api")
	if start < 0 || end < 0 {
		t.Fatal("cannot find the Supersede step's probe in agent-review.yml")
	}
	var probe strings.Builder
	for line := range strings.SplitSeq(text[start:start+1+end], "\n") {
		probe.WriteString(strings.TrimPrefix(line, "          ") + "\n")
	}
	probe.WriteString("echo PAST-THE-PROBE\n")

	run := func(t *testing.T, script string) (string, int) {
		t.Helper()
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "trusted", "scripts"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "trusted", "scripts", "agent-review.sh"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("bash", "-e", "-c", probe.String())
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		code := 0
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return string(out), code
	}

	real, err := os.ReadFile("agent-review.sh")
	if err != nil {
		t.Fatal(err)
	}
	if out, code := run(t, string(real)); code != 0 || !strings.Contains(out, "PAST-THE-PROBE") || strings.Contains(out, "cannot supersede") {
		t.Errorf("a script with supersede: exit %d, output %q; want the step to go on past the probe", code, out)
	}
	// A default branch's script from before supersede existed: usage exit 64.
	old := "#!/usr/bin/env bash\nprintf 'usage\\n' >&2\nexit 64\n"
	out, code := run(t, old)
	if code != 0 || !strings.Contains(out, "cannot supersede yet") || strings.Contains(out, "PAST-THE-PROBE") {
		t.Errorf("a script without supersede: exit %d, output %q; want the warning and a clean exit before the step goes on", code, out)
	}
}
