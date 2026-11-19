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

// TestAgentReviewLensesFilterByDiff checks that a diff with no Go or web code
// returns neighbor only, and a Go or .tsx diff returns all lenses.
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
// bold title (from the title field, or rule if absent) and have reasoning in
// a <details> block.
func TestAgentReviewInlineCommentTitle(t *testing.T) {
	// With title field present: comment opens with that title.
	withTitle := reviewOf(t, `{"head_sha":"h","expected":["a"],"lenses":[{"name":"a","findings":[
		{"severity":"blocking","path":"x.go","line":5,"rule":"mocked-sut","title":"Use testcontainers","failure":"mocks the db","fix":"use real db"}
	]}]}`)
	if len(withTitle.Comments) != 1 || !strings.HasPrefix(withTitle.Comments[0].Body, "**Use testcontainers**") {
		t.Fatalf("title field: comment body = %q", withTitle.Comments[0].Body)
	}
	if !strings.Contains(withTitle.Comments[0].Body, "<details>") || !strings.Contains(withTitle.Comments[0].Body, "mocks the db") {
		t.Fatalf("details block: comment body = %q", withTitle.Comments[0].Body)
	}

	// Without title field: falls back to rule.
	noTitle := reviewOf(t, `{"head_sha":"h","expected":["a"],"lenses":[{"name":"a","findings":[
		{"severity":"blocking","path":"x.go","line":5,"rule":"mocked-sut","failure":"mocks the db","fix":"use real db"}
	]}]}`)
	if len(noTitle.Comments) != 1 || !strings.HasPrefix(noTitle.Comments[0].Body, "**mocked-sut**") {
		t.Fatalf("rule fallback: comment body = %q", noTitle.Comments[0].Body)
	}
}

// TestAgentReviewDeduplicatesOpenFindings checks that a finding whose path,
// line and rule match an existing reviewer thread is dropped from the review.
func TestAgentReviewDeduplicatesOpenFindings(t *testing.T) {
	results := `{
		"head_sha":"h","expected":["a"],
		"open_threads":[{"path":"x.go","line":5,"rule":"r"}],
		"lenses":[{"name":"a","findings":[
			{"severity":"blocking","path":"x.go","line":5,"rule":"r","failure":"f1","fix":"x1"},
			{"severity":"advisory","path":"y.go","line":3,"rule":"s","failure":"f2","fix":"x2"}
		]}]}`
	r := reviewOf(t, results)
	// x.go:5 matches the open thread and is dropped; only y.go:3 survives.
	if r.Event != "COMMENT" {
		t.Fatalf("event = %s, want COMMENT (blocking finding was deduplicated)", r.Event)
	}
	if len(r.Comments) != 1 || r.Comments[0].Path != "y.go" || r.Comments[0].Line != 3 {
		t.Fatalf("comments = %+v, want only y.go:3", r.Comments)
	}
	if !strings.Contains(r.Body, "1 advisory") {
		t.Fatalf("body = %q, want 1 advisory", r.Body)
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

// TestAgentReviewIncrementalBase checks that the most recent reviewer review's
// commit_id is returned, or "" when the reviewer has not reviewed yet.
func TestAgentReviewIncrementalBase(t *testing.T) {
	// Most recent reviewer review is the last in the array.
	multi := `{"reviewer_bot":"reviewer[bot]","reviews":[
		{"user":"reviewer[bot]","state":"CHANGES_REQUESTED","commit_id":"sha1"},
		{"user":"reviewer[bot]","state":"COMMENTED","commit_id":"sha2"}
	]}`
	got, code := agentReview(t, multi, "incremental-base")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, got)
	}
	if got != "sha2" {
		t.Fatalf("got %q, want sha2 (most recent reviewer review)", got)
	}

	// No reviews from the reviewer bot.
	noBot := `{"reviewer_bot":"reviewer[bot]","reviews":[{"user":"other-user","state":"APPROVED","commit_id":"sha1"}]}`
	if got2, _ := agentReview(t, noBot, "incremental-base"); got2 != "" {
		t.Fatalf("no reviewer reviews: got %q, want empty", got2)
	}

	// Empty reviews array.
	if got3, _ := agentReview(t, `{"reviewer_bot":"reviewer[bot]","reviews":[]}`, "incremental-base"); got3 != "" {
		t.Fatalf("empty reviews: got %q, want empty", got3)
	}

	// Case-insensitive bot login match.
	ci := `{"reviewer_bot":"Reviewer[Bot]","reviews":[{"user":"reviewer[bot]","state":"APPROVED","commit_id":"sha3"}]}`
	if got4, _ := agentReview(t, ci, "incremental-base"); got4 != "sha3" {
		t.Fatalf("case mismatch: got %q, want sha3", got4)
	}

	// Bad input.
	if _, code5 := agentReview(t, `[]`, "incremental-base"); code5 != 2 {
		t.Fatalf("array input: exit %d, want 2", code5)
	}
	if _, code6 := agentReview(t, "", "incremental-base", "extra"); code6 != 64 {
		t.Fatalf("extra arg: exit %d, want 64", code6)
	}
}
