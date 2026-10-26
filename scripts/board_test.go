// Package scripts holds the tests of the repo's standalone scripts. These drive
// scripts/board.sh, the one way an agent moves the project board (#1191),
// against a fake gh: no call reaches GitHub.
//
// The fake answers from fixtures recorded off org project #1
// (testdata/board/field-list.json from gh project field-list, project-list.json
// from gh project list with the org's other project redacted, issue-item.json
// from the script's own issue query) and keeps the board's state in a file
// between runs, so a second run reads what the first wrote. It refuses what GitHub would refuse (an id the project does not
// have, an issue the repo does not have, a call it does not know), so a script
// that resolved a name to the wrong id fails here rather than on the live board.
// The fake is this test binary, re-entered through a gh shim on PATH (TestMain).
package scripts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	issueURLPrefix = "https://github.com/hyperscaleav/omniglass/issues/"
	pinnedToday    = "2026-10-26" // inside Iteration 4 of testdata/board/iterations.json
	// injectedInReviewID is the "In Review" option a test board gains when the
	// recording has none (the board as it stands before the one-time setup).
	injectedInReviewID = "a7e3c9d1"
)

// The ids the script must resolve by name, read out of the recorded fixtures
// by the same names (init), so a board recorded again keeps the tests honest
// rather than pinning the ids of the day it was first recorded.
var (
	projectID, statusFieldID, priorityFieldID, iterationFieldID string
	backlogID, todoID, inProgressID                             string
)

func init() {
	var projects struct {
		Projects []struct{ ID, Title string } `json:"projects"`
	}
	raw, err := fixture("project-list.json")
	if err == nil {
		err = json.Unmarshal([]byte(raw), &projects)
	}
	for _, pr := range projects.Projects {
		if pr.Title == "Omniglass" {
			projectID = pr.ID
		}
	}
	f, ferr := fields(&boardState{})
	if err == nil {
		err = ferr
	}
	if err != nil || projectID == "" {
		panic(fmt.Sprintf("the recorded board fixtures do not load: %v (project %q)", err, projectID))
	}
	statusFieldID, priorityFieldID, iterationFieldID = f.fieldID("Status"), f.fieldID("Priority"), f.fieldID("Iteration")
	backlogID, todoID, inProgressID = f.optionID("Status", "Backlog"), f.optionID("Status", "Todo"), f.optionID("Status", "In Progress")
}

// fakeEnv names the fake's state directory; set, the test binary is gh.
const fakeEnv = "BOARD_FAKE_GH"

func TestMain(m *testing.M) {
	if dir := os.Getenv(fakeEnv); dir != "" {
		os.Exit(fakeGH(dir, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// ---- the harness -------------------------------------------------------------

type board struct {
	t   *testing.T
	dir string
	env []string
}

// newBoard is a board holding st, with gh on PATH answering for it.
func newBoard(t *testing.T, st boardState) *board {
	t.Helper()
	requireTools(t)
	dir := t.TempDir()
	if st.Iterations == "" {
		st.Iterations = "iterations.json"
	}
	if st.Issues == nil {
		st.Issues = map[string]*issue{}
	}
	if err := writeState(dir, &st); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(dir, "bin")
	if err := os.Mkdir(shim, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shim, "gh"), []byte("#!/bin/sh\nexec '"+exe+"' \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	fixtures, err := filepath.Abs(filepath.Join("testdata", "board"))
	if err != nil {
		t.Fatal(err)
	}
	env := []string{}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "BOARD_") && !strings.HasPrefix(kv, "PATH=") {
			env = append(env, kv)
		}
	}
	env = append(env,
		"PATH="+shim+string(os.PathListSeparator)+os.Getenv("PATH"),
		fakeEnv+"="+dir,
		"BOARD_FAKE_FIXTURES="+fixtures,
		"BOARD_TODAY="+pinnedToday,
		"BOARD_SETTLE=0", // no wait for a workflow unless a test models one
	)
	return &board{t: t, dir: dir, env: env}
}

// requireTools skips, naming why, on a host without what board.sh needs (bash,
// and jq, which a stock desktop may lack); in CI, where both are installed, a
// missing one fails instead, so the gate never goes quiet.
func requireTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			if os.Getenv("CI") != "" {
				t.Fatalf("%s is not on PATH: scripts/board.sh and its tests need it", tool)
			}
			t.Skipf("%s is not on PATH: scripts/board.sh needs it, so its tests skip here (install %s to run them)", tool, tool)
		}
	}
}

type result struct {
	code           int
	stdout, stderr string
	calls          [][]string
}

// run runs board.sh once with extra env (KEY=VALUE) and returns what it printed
// and the gh calls it made. Every run is held to the invariant: no call ever
// touches Priority.
func (b *board) run(extra []string, args ...string) result {
	b.t.Helper()
	if err := os.Remove(filepath.Join(b.dir, "calls.jsonl")); err != nil && !errors.Is(err, os.ErrNotExist) {
		b.t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{"board.sh"}, args...)...)
	cmd.Env = append(append([]string{}, b.env...), extra...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	r := result{}
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			b.t.Fatal(err)
		}
		r.code = ee.ExitCode()
	}
	r.stdout, r.stderr = out.String(), errb.String()
	if raw, err := os.ReadFile(filepath.Join(b.dir, "calls.jsonl")); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			var c []string
			if err := json.Unmarshal([]byte(line), &c); err != nil {
				b.t.Fatal(err)
			}
			r.calls = append(r.calls, c)
		}
	}
	for _, c := range r.calls {
		joined := strings.Join(c, " ")
		if strings.Contains(joined, priorityFieldID) || strings.Contains(joined, "--field Priority") {
			b.t.Errorf("board.sh %q touched Priority: gh %s", args, joined)
		}
		if strings.Contains(joined, "fake gh: unexpected call") {
			b.t.Errorf("board.sh %q made a call the fake does not know: gh %s", args, joined)
		}
	}
	if strings.Contains(r.stderr, "fake gh: unexpected call") {
		b.t.Errorf("board.sh %q made a call the fake does not know:\n%s", args, r.stderr)
	}
	return r
}

func (b *board) state() *boardState {
	b.t.Helper()
	st, err := readState(b.dir)
	if err != nil {
		b.t.Fatal(err)
	}
	return st
}

// writes are the calls that change the board.
func (r result) writes() [][]string {
	var w [][]string
	for _, c := range r.calls {
		if len(c) >= 2 && c[0] == "project" && (c[1] == "item-add" || c[1] == "item-edit" || c[1] == "item-archive") {
			w = append(w, c)
		}
	}
	return w
}

func (r result) count(words ...string) int {
	n := 0
	for _, c := range r.calls {
		if len(c) >= len(words) && strings.Join(c[:len(words)], " ") == strings.Join(words, " ") {
			n++
		}
	}
	return n
}

// flag is the value after name in a gh call.
func flag(call []string, name string) string {
	for i := 0; i+1 < len(call); i++ {
		if call[i] == name {
			return call[i+1]
		}
	}
	return ""
}

func onBoard(title string, it item) *issue { return &issue{Title: title, Item: &it} }

func (r result) wantCode(t *testing.T, code int) {
	t.Helper()
	if r.code != code {
		t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", r.code, code, r.stdout, r.stderr)
	}
}

func wantContains(t *testing.T, what, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%s = %q, want it to contain %q", what, got, want)
	}
}

// wantStatusEdit asserts the one write is an item-edit of item's Status to option.
func wantStatusEdit(t *testing.T, w []string, itemID, option string) {
	t.Helper()
	if strings.Join(w[:2], " ") != "project item-edit" ||
		flag(w, "--id") != itemID || flag(w, "--project-id") != projectID ||
		flag(w, "--field-id") != statusFieldID || flag(w, "--single-select-option-id") != option {
		t.Errorf("write = gh %s, want item-edit of %s's Status (%s) to option %s in %s",
			strings.Join(w, " "), itemID, statusFieldID, option, projectID)
	}
}

// ---- argument handling -------------------------------------------------------

func TestBoardArguments(t *testing.T) {
	for _, tc := range []struct {
		args     []string
		code     int
		out, err string
	}{
		{nil, 2, "", "a command is required"},
		{[]string{"help"}, 0, "usage: board.sh", ""},
		{[]string{"-h"}, 0, "usage: board.sh", ""},
		{[]string{"--help"}, 0, "usage: board.sh", ""},
		{[]string{"move", "12"}, 2, "", "unknown command: move"},
		{[]string{"add"}, 2, "", "add takes one issue number"},
		{[]string{"add", "12", "13"}, 2, "", "add takes one issue number"},
		{[]string{"add", "abc"}, 2, "", "not an issue number: abc"},
		{[]string{"add", "0"}, 2, "", "not an issue number: 0"},
		{[]string{"add", "12a"}, 2, "", "not an issue number: 12a"},
		{[]string{"show"}, 2, "", "show takes one issue number"},
		{[]string{"set", "12"}, 2, "", "set takes an issue number and a status"},
		{[]string{"set", "x", "Todo"}, 2, "", "not an issue number: x"},
		{[]string{"iteration", "12"}, 2, "", "iteration takes an issue number and current"},
		{[]string{"iteration", "12", "next"}, 2, "", "iteration takes current"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			r := newBoard(t, boardState{}).run(nil, tc.args...)
			r.wantCode(t, tc.code)
			wantContains(t, "stdout", r.stdout, tc.out)
			wantContains(t, "stderr", r.stderr, tc.err)
			if tc.code == 2 {
				wantContains(t, "stderr", r.stderr, "usage: board.sh")
			}
			if len(r.calls) != 0 {
				t.Errorf("an argument error called gh: %q", r.calls)
			}
		})
	}
}

func TestBoardAcceptsAHashedIssueNumber(t *testing.T) {
	b := newBoard(t, boardState{Issues: map[string]*issue{
		"1145": onBoard("feat: a slice", item{ID: "PVTI_1145", Status: "Backlog"}),
	}})
	r := b.run(nil, "set", "#1145", "Todo")
	r.wantCode(t, 0)
	if got := b.state().Issues["1145"].Item.Status; got != "Todo" {
		t.Errorf("status = %q, want Todo", got)
	}
}

// ---- set: name to id, idempotence, the In Review fallback, never Priority ----

func TestBoardSetResolvesNamesToTheRecordedIDs(t *testing.T) {
	b := newBoard(t, boardState{Issues: map[string]*issue{
		"1145": onBoard("feat: a slice", item{ID: "PVTI_1145", Status: "Backlog", Priority: "Medium"}),
	}})
	r := b.run(nil, "set", "1145", "In Progress")
	r.wantCode(t, 0)
	wantContains(t, "stdout", r.stdout, "#1145: Backlog -> In Progress")
	w := r.writes()
	if len(w) != 1 {
		t.Fatalf("writes = %q, want one item-edit", w)
	}
	wantStatusEdit(t, w[0], "PVTI_1145", inProgressID)
	if r.count("project", "list") != 1 || r.count("project", "field-list") != 1 {
		t.Errorf("project list x%d, field-list x%d: want each resolved once per run",
			r.count("project", "list"), r.count("project", "field-list"))
	}
	if got := b.state().Issues["1145"].Item; got.Status != "In Progress" || got.Priority != "Medium" {
		t.Errorf("item = %+v, want In Progress with Priority untouched", got)
	}
}

func TestBoardSetTwiceChangesNothing(t *testing.T) {
	b := newBoard(t, boardState{Issues: map[string]*issue{
		"1145": onBoard("feat: a slice", item{ID: "PVTI_1145", Status: "Backlog"}),
	}})
	b.run(nil, "set", "1145", "In Progress").wantCode(t, 0)
	r := b.run(nil, "set", "1145", "In Progress")
	r.wantCode(t, 0)
	wantContains(t, "stdout", r.stdout, "#1145 is already In Progress: unchanged")
	if w := r.writes(); len(w) != 0 {
		t.Errorf("the second run wrote %q, want nothing", w)
	}
}

func TestBoardSetMatchesAStatusIgnoringCaseAndQuotes(t *testing.T) {
	for _, args := range [][]string{
		{"set", "1145", "in progress"},
		{"set", "1145", "In", "Progress"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			b := newBoard(t, boardState{Issues: map[string]*issue{
				"1145": onBoard("feat: a slice", item{ID: "PVTI_1145", Status: "Todo"}),
			}})
			r := b.run(nil, args...)
			r.wantCode(t, 0)
			wantContains(t, "stdout", r.stdout, "#1145: Todo -> In Progress")
			if w := r.writes(); len(w) != 1 {
				t.Fatalf("writes = %q, want one", w)
			} else {
				wantStatusEdit(t, w[0], "PVTI_1145", inProgressID)
			}
		})
	}
}

func TestBoardSetRefusesAStatusTheBoardLacks(t *testing.T) {
	b := newBoard(t, boardState{Issues: map[string]*issue{
		"1145": onBoard("feat: a slice", item{ID: "PVTI_1145", Status: "Todo"}),
	}})
	r := b.run(nil, "set", "1145", "Blocked")
	r.wantCode(t, 2)
	wantContains(t, "stderr", r.stderr, `no status "Blocked" on Omniglass (Status: `+statusNames(t, boardState{})+`)`)
	if w := r.writes(); len(w) != 0 {
		t.Errorf("wrote %q, want nothing", w)
	}
}

func TestBoardNeverSetsPriority(t *testing.T) {
	b := newBoard(t, boardState{Issues: map[string]*issue{
		"1145": onBoard("feat: a slice", item{ID: "PVTI_1145", Status: "Todo"}),
	}})
	r := b.run(nil, "set", "1145", "High")
	r.wantCode(t, 2)
	wantContains(t, "stderr", r.stderr, "High is a Priority, which the architect alone sets")
	if w := r.writes(); len(w) != 0 {
		t.Errorf("wrote %q, want nothing", w)
	}
}

func TestBoardInReviewFallsBackToInProgressWithOneWarning(t *testing.T) {
	b := newBoard(t, boardState{Issues: map[string]*issue{
		"1145": onBoard("feat: a slice", item{ID: "PVTI_1145", Status: "Todo"}),
	}})
	r := b.run(nil, "set", "1145", "In Review")
	r.wantCode(t, 0)
	if n := strings.Count(r.stderr, "warning"); n != 1 {
		t.Errorf("stderr warned %d times, want once:\n%s", n, r.stderr)
	}
	wantContains(t, "stderr", r.stderr, `no "In Review" status yet`)
	wantContains(t, "stdout", r.stdout, "#1145: Todo -> In Progress")
	if w := r.writes(); len(w) != 1 {
		t.Fatalf("writes = %q, want one", w)
	} else {
		wantStatusEdit(t, w[0], "PVTI_1145", inProgressID)
	}

	again := b.run(nil, "set", "1145", "In Review")
	again.wantCode(t, 0)
	wantContains(t, "stdout", again.stdout, "#1145 is already In Progress: unchanged")
	if w := again.writes(); len(w) != 0 {
		t.Errorf("the second run wrote %q, want nothing", w)
	}
}

func TestBoardInReviewLandsWhenTheBoardHasIt(t *testing.T) {
	b := newBoard(t, boardState{InReview: true, Issues: map[string]*issue{
		"1145": onBoard("feat: a slice", item{ID: "PVTI_1145", Status: "In Progress"}),
	}})
	r := b.run(nil, "set", "1145", "In Review")
	r.wantCode(t, 0)
	if strings.Contains(r.stderr, "warning") {
		t.Errorf("warned with In Review on the board:\n%s", r.stderr)
	}
	wantContains(t, "stdout", r.stdout, "#1145: In Progress -> In Review")
	if w := r.writes(); len(w) != 1 {
		t.Fatalf("writes = %q, want one", w)
	} else {
		inReview, _ := fields(&boardState{InReview: true})
		wantStatusEdit(t, w[0], "PVTI_1145", inReview.optionID("Status", "In Review"))
	}
}

func TestBoardSetAddsAnIssueNotOnTheBoard(t *testing.T) {
	b := newBoard(t, boardState{Issues: map[string]*issue{"1191": {Title: "feat: the board"}}})
	r := b.run(nil, "set", "1191", "Todo")
	r.wantCode(t, 0)
	wantContains(t, "stdout", r.stdout, "#1191 added to Omniglass")
	wantContains(t, "stdout", r.stdout, "#1191: (none) -> Todo")
	w := r.writes()
	if len(w) != 2 || strings.Join(w[0][:2], " ") != "project item-add" || flag(w[0], "--url") != issueURLPrefix+"1191" {
		t.Fatalf("writes = %q, want item-add of the issue's url, then item-edit", w)
	}
	wantStatusEdit(t, w[1], "PVTI_fake1", todoID)
}

func TestBoardRestoresAnArchivedItem(t *testing.T) {
	b := newBoard(t, boardState{Issues: map[string]*issue{
		"1145": onBoard("feat: a slice", item{ID: "PVTI_1145", Status: "Todo", Archived: true}),
	}})
	r := b.run(nil, "set", "1145", "In Progress")
	r.wantCode(t, 0)
	wantContains(t, "stdout", r.stdout, "#1145 restored from the archive")
	w := r.writes()
	if len(w) != 2 || strings.Join(w[0][:2], " ") != "project item-archive" || flag(w[0], "--id") != "PVTI_1145" || w[0][len(w[0])-1] != "--undo" {
		t.Fatalf("writes = %q, want item-archive --undo of PVTI_1145, then item-edit", w)
	}
	wantStatusEdit(t, w[1], "PVTI_1145", inProgressID)
	if got := b.state().Issues["1145"].Item; got.Archived || got.Status != "In Progress" {
		t.Errorf("item = %+v, want restored and In Progress", got)
	}
}

// ---- add -------------------------------------------------------------------

func TestBoardAddPutsAnIssueInBacklogOnce(t *testing.T) {
	b := newBoard(t, boardState{Issues: map[string]*issue{"1191": {Title: "feat: the board"}}})
	r := b.run(nil, "add", "1191")
	r.wantCode(t, 0)
	wantContains(t, "stdout", r.stdout, "#1191 added to Omniglass")
	wantContains(t, "stdout", r.stdout, "#1191: (none) -> Backlog")
	w := r.writes()
	if len(w) != 2 || strings.Join(w[0][:2], " ") != "project item-add" {
		t.Fatalf("writes = %q, want item-add then item-edit", w)
	}
	wantStatusEdit(t, w[1], "PVTI_fake1", backlogID)
	if got := b.state().Issues["1191"].Item; got.Status != "Backlog" || got.Priority != "" {
		t.Errorf("item = %+v, want Backlog with Priority blank", got)
	}

	again := b.run(nil, "add", "1191")
	again.wantCode(t, 0)
	wantContains(t, "stdout", again.stdout, "#1191 is already on Omniglass (Backlog): unchanged")
	if w := again.writes(); len(w) != 0 {
		t.Errorf("the second add wrote %q, want nothing", w)
	}
}

func TestBoardAddLeavesAnItemsStatusAlone(t *testing.T) {
	b := newBoard(t, boardState{Issues: map[string]*issue{
		"1145": onBoard("feat: a slice", item{ID: "PVTI_1145", Status: "In Progress"}),
	}})
	r := b.run(nil, "add", "1145")
	r.wantCode(t, 0)
	wantContains(t, "stdout", r.stdout, "#1145 is already on Omniglass (In Progress): unchanged")
	if w := r.writes(); len(w) != 0 {
		t.Errorf("wrote %q, want nothing", w)
	}
}

func TestBoardAddGivesAStatuslessItemBacklog(t *testing.T) {
	b := newBoard(t, boardState{Issues: map[string]*issue{
		"1145": onBoard("feat: a slice", item{ID: "PVTI_1145"}),
	}})
	r := b.run(nil, "add", "1145")
	r.wantCode(t, 0)
	if w := r.writes(); len(w) != 1 {
		t.Fatalf("writes = %q, want one item-edit", w)
	} else {
		wantStatusEdit(t, w[0], "PVTI_1145", backlogID)
	}
}

// ---- iteration -------------------------------------------------------------

func TestBoardIterationCurrent(t *testing.T) {
	for _, tc := range []struct {
		today, id, title string
	}{
		{pinnedToday, "4c1d2e3f", "Iteration 4"},
		{"2026-10-18", "4c1d2e3f", "Iteration 4"}, // its first day
		{"2026-10-31", "4c1d2e3f", "Iteration 4"}, // its last day
		{"2026-11-01", "5d2e3f40", "Iteration 5"}, // the next one's first
	} {
		t.Run(tc.today, func(t *testing.T) {
			b := newBoard(t, boardState{Issues: map[string]*issue{
				"1145": onBoard("feat: a slice", item{ID: "PVTI_1145", Status: "Todo"}),
			}})
			today := []string{"BOARD_TODAY=" + tc.today}
			r := b.run(today, "iteration", "1145", "current")
			r.wantCode(t, 0)
			wantContains(t, "stdout", r.stdout, "#1145: Iteration (none) -> "+tc.title)
			w := r.writes()
			if len(w) != 1 || flag(w[0], "--field-id") != iterationFieldID || flag(w[0], "--iteration-id") != tc.id || flag(w[0], "--project-id") != projectID {
				t.Fatalf("writes = %q, want item-edit of the Iteration field to %s", w, tc.id)
			}
			again := b.run(today, "iteration", "1145", "current")
			again.wantCode(t, 0)
			wantContains(t, "stdout", again.stdout, "#1145 is already in "+tc.title+": unchanged")
			if w := again.writes(); len(w) != 0 {
				t.Errorf("the second run wrote %q, want nothing", w)
			}
		})
	}
}

func TestBoardIterationWithNoneRunning(t *testing.T) {
	b := newBoard(t, boardState{Iterations: "iterations-none.json", Issues: map[string]*issue{
		"1145": onBoard("feat: a slice", item{ID: "PVTI_1145", Status: "Todo"}),
	}})
	r := b.run(nil, "iteration", "1145", "current")
	r.wantCode(t, 1)
	wantContains(t, "stderr", r.stderr, "no iteration of Omniglass is running on "+pinnedToday)
	if w := r.writes(); len(w) != 0 {
		t.Errorf("wrote %q, want nothing", w)
	}
}

// ---- show ------------------------------------------------------------------

func TestBoardShow(t *testing.T) {
	b := newBoard(t, boardState{Issues: map[string]*issue{
		"1145": onBoard("feat: a slice", item{ID: "PVTI_1145", Status: "In Progress", Priority: "Medium"}),
	}})
	r := b.run(nil, "show", "1145")
	r.wantCode(t, 0)
	for _, want := range []string{"#1145 feat: a slice", "Status:     In Progress", "Priority:   Medium", "Iteration:  (none)"} {
		wantContains(t, "stdout", r.stdout, want)
	}
	if w := r.writes(); len(w) != 0 {
		t.Errorf("show wrote %q", w)
	}
}

func TestBoardShowAnIssueNotOnTheBoard(t *testing.T) {
	b := newBoard(t, boardState{Issues: map[string]*issue{"1191": {Title: "feat: the board"}}})
	r := b.run(nil, "show", "1191")
	r.wantCode(t, 1)
	wantContains(t, "stderr", r.stderr, "#1191 is not on Omniglass (scripts/board.sh add 1191 puts it there)")
	if w := r.writes(); len(w) != 0 {
		t.Errorf("show wrote %q", w)
	}
}

// ---- the errors a caller can act on ----------------------------------------

func TestBoardNamesAMissingIssue(t *testing.T) {
	for _, args := range [][]string{{"show", "99999"}, {"set", "99999", "Todo"}, {"add", "99999"}} {
		t.Run(args[0], func(t *testing.T) {
			r := newBoard(t, boardState{}).run(nil, args...)
			r.wantCode(t, 1)
			wantContains(t, "stderr", r.stderr, "no issue #99999 in hyperscaleav/omniglass")
			if w := r.writes(); len(w) != 0 {
				t.Errorf("wrote %q, want nothing", w)
			}
		})
	}
}

func TestBoardNamesAMissingScope(t *testing.T) {
	r := newBoard(t, boardState{ScopeMissing: true}).run(nil, "set", "1145", "Todo")
	r.wantCode(t, 1)
	wantContains(t, "stderr", r.stderr, "the gh token lacks the project scope; run: gh auth refresh -s project")
	if w := r.writes(); len(w) != 0 {
		t.Errorf("wrote %q, want nothing", w)
	}
}

func TestBoardNamesAMissingProject(t *testing.T) {
	r := newBoard(t, boardState{}).run([]string{"BOARD_PROJECT=Nowhere"}, "show", "1145")
	r.wantCode(t, 1)
	wantContains(t, "stderr", r.stderr, `no project named "Nowhere" under hyperscaleav (it has: Another project, Omniglass)`)
}

// ---- the board's own workflows ---------------------------------------------

// The board's "Item added to project" workflow gives a new item a Status a
// moment after the add. A move written before it lands would be overwritten, so
// set waits for it and then writes.
func TestBoardSetWaitsForTheBoardsAddedWorkflow(t *testing.T) {
	b := newBoard(t, boardState{AddedStatus: "Backlog", AddedAfterReads: 1,
		Issues: map[string]*issue{"1191": {Title: "feat: the board"}}})
	r := b.run([]string{"BOARD_SETTLE=5"}, "set", "1191", "Todo")
	r.wantCode(t, 0)
	wantContains(t, "stdout", r.stdout, "#1191: Backlog -> Todo")
	w := r.writes()
	if len(w) != 2 || w[0][1] != "item-add" {
		t.Fatalf("writes = %q, want item-add then item-edit", w)
	}
	wantStatusEdit(t, w[1], "PVTI_fake1", todoID)
	// The edit comes after the read that saw the workflow's Backlog: the
	// lookup, a read with no Status yet, then the read with it.
	if n := r.count("api", "graphql"); n != 3 {
		t.Errorf("issue reads = %d, want 3 (lookup, then reads until the workflow's Status shows)", n)
	}
	if last := r.calls[len(r.calls)-1]; last[1] != "item-edit" {
		t.Errorf("last call = %q, want the item-edit after the workflow landed", last)
	}
	if got := b.state().Issues["1191"].Item.Status; got != "Todo" {
		t.Errorf("status = %q, want Todo", got)
	}
}

func TestBoardAddKeepsTheStatusTheWorkflowGave(t *testing.T) {
	b := newBoard(t, boardState{AddedStatus: "Backlog",
		Issues: map[string]*issue{"1191": {Title: "feat: the board"}}})
	r := b.run([]string{"BOARD_SETTLE=5"}, "add", "1191")
	r.wantCode(t, 0)
	wantContains(t, "stdout", r.stdout, "#1191: Backlog, from the board's own workflow: unchanged")
	if w := r.writes(); len(w) != 1 || w[0][1] != "item-add" {
		t.Errorf("writes = %q, want the item-add alone", w)
	}
}

// With the workflow off, the wait runs out and the move is written.
func TestBoardSetWritesWhenNoWorkflowLands(t *testing.T) {
	b := newBoard(t, boardState{Issues: map[string]*issue{"1191": {Title: "feat: the board"}}})
	r := b.run([]string{"BOARD_SETTLE=1"}, "set", "1191", "Todo")
	r.wantCode(t, 0)
	wantContains(t, "stdout", r.stdout, "#1191: (none) -> Todo")
	if n := r.count("api", "graphql"); n != 2 {
		t.Errorf("issue reads = %d, want 2 (lookup, one read in a 1s wait)", n)
	}
	if w := r.writes(); len(w) != 2 {
		t.Fatalf("writes = %q, want item-add then item-edit", w)
	} else {
		wantStatusEdit(t, w[1], "PVTI_fake1", todoID)
	}
}

func TestBoardRefusesABadSettle(t *testing.T) {
	r := newBoard(t, boardState{}).run([]string{"BOARD_SETTLE=soon"}, "add", "1191")
	r.wantCode(t, 2)
	wantContains(t, "stderr", r.stderr, "BOARD_SETTLE is not a number of seconds: soon")
	if len(r.calls) != 0 {
		t.Errorf("called gh: %q", r.calls)
	}
}

// The board recorded again after the one-time setup has an "In Review" option
// of its own; the harness keeps that id for a board with it and strips it for
// one without, so recording again turns no test red.
func TestHarnessOwnsTheInReviewVariant(t *testing.T) {
	raw, err := fixture("field-list.json")
	if err != nil {
		t.Fatal(err)
	}
	var f map[string]any
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		t.Fatal(err)
	}
	for _, fd := range f["fields"].([]any) {
		m := fd.(map[string]any)
		if m["name"] == "Status" {
			opts := m["options"].([]any)
			m["options"] = append(opts[:len(opts)-1:len(opts)-1], map[string]any{"id": "c0ffee00", "name": "In Review"}, opts[len(opts)-1])
		}
	}
	dir := t.TempDir()
	b, _ := json.Marshal(f)
	if err := os.WriteFile(filepath.Join(dir, "field-list.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BOARD_FAKE_FIXTURES", dir)
	without, err := fields(&boardState{})
	if err != nil {
		t.Fatal(err)
	}
	if id := without.optionID("Status", "In Review"); id != "" {
		t.Errorf("a board without In Review still has it (%s)", id)
	}
	with, err := fields(&boardState{InReview: true})
	if err != nil {
		t.Fatal(err)
	}
	if id := with.optionID("Status", "In Review"); id != "c0ffee00" {
		t.Errorf("In Review = %q, want the recording's own c0ffee00", id)
	}
	names := strings.Split(statusNames(t, boardState{InReview: true}), ", ")
	at, n := -1, 0
	for i, name := range names {
		if name == "In Review" {
			at, n = i, n+1
		}
	}
	if n != 1 || at+1 >= len(names) || names[at+1] != "Done" {
		t.Errorf("statuses = %q, want In Review once, just before Done", names)
	}
}
