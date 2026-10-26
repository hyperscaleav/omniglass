// Package scripts holds the tests of the repo's standalone scripts. These drive
// scripts/board.sh, the one way an agent moves the project board (#1191),
// against a fake gh: no call reaches GitHub.
//
// The fake answers from fixtures recorded off org project #1
// (testdata/board/field-list.json from gh project field-list, project-list.json
// from gh project list, issue-item.json from the script's own issue query) and
// keeps the board's state in a file between runs, so a second run reads what the
// first wrote. It refuses what GitHub would refuse (an id the project does not
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
	"strconv"
	"strings"
	"testing"
)

// The ids the recorded fixtures carry, which the script must resolve by name.
const (
	projectID        = "PVT_kwDOC43oI84BOqlh"
	statusFieldID    = "PVTSSF_lADOC43oI84BOqlhzg9TL6M"
	priorityFieldID  = "PVTSSF_lADOC43oI84BOqlhzhW2YIc"
	iterationFieldID = "PVTIF_lADOC43oI84BOqlhzg9TR20"
	backlogID        = "b0771673"
	todoID           = "f75ad846"
	inProgressID     = "47fc9ee4"
	inReviewID       = "a7e3c9d1" // added to the recorded Status options when a test's board has "In Review"
	issueURLPrefix   = "https://github.com/hyperscaleav/omniglass/issues/"
	pinnedToday      = "2026-10-26" // inside Iteration 4 of testdata/board/iterations.json
)

// fakeEnv names the fake's state directory; set, the test binary is gh.
const fakeEnv = "BOARD_FAKE_GH"

func TestMain(m *testing.M) {
	if dir := os.Getenv(fakeEnv); dir != "" {
		os.Exit(fakeGH(dir, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// ---- the board's state -----------------------------------------------------

type boardState struct {
	InReview     bool              `json:"in_review"`     // the Status field has an "In Review" option
	ScopeMissing bool              `json:"scope_missing"` // the token lacks the project scope
	Iterations   string            `json:"iterations"`    // the iterations fixture the board answers with
	Issues       map[string]*issue `json:"issues"`        // the repo's issues, by number
	NextItem     int               `json:"next_item"`
}

type issue struct {
	Title string `json:"title"`
	Item  *item  `json:"item,omitempty"` // nil: not on the board
}

type item struct {
	ID        string `json:"id"`
	Archived  bool   `json:"archived"`
	Status    string `json:"status"`
	Priority  string `json:"priority"`
	Iteration string `json:"iteration"`
}

func readState(dir string) (*boardState, error) {
	b, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		return nil, err
	}
	var st boardState
	return &st, json.Unmarshal(b, &st)
}

func writeState(dir string, st *boardState) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "state.json"), b, 0o644)
}

// ---- the fake gh -------------------------------------------------------------

// fakeGH answers one gh invocation and returns its exit code.
func fakeGH(dir string, args []string) int {
	st, err := readState(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake gh: state:", err)
		return 98
	}
	if err := logCall(dir, args); err != nil {
		fmt.Fprintln(os.Stderr, "fake gh: log:", err)
		return 98
	}
	out, err := answer(st, args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := writeState(dir, st); err != nil {
		fmt.Fprintln(os.Stderr, "fake gh: state:", err)
		return 98
	}
	fmt.Print(out)
	return 0
}

func logCall(dir string, args []string) error {
	f, err := os.OpenFile(filepath.Join(dir, "calls.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, _ := json.Marshal(args)
	_, err = fmt.Fprintf(f, "%s\n", b)
	return err
}

// parsed is one gh invocation: its subcommand words, its --flag values, its
// -f/-F fields, and its bare switches (--undo).
type parsed struct {
	words    []string
	flags    map[string]string
	fields   map[string]string
	switches map[string]bool
}

func parse(args []string) parsed {
	p := parsed{flags: map[string]string{}, fields: map[string]string{}, switches: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-f" || a == "-F":
			if i+1 < len(args) {
				k, v, _ := strings.Cut(args[i+1], "=")
				p.fields[k] = v
				i++
			}
		case a == "--undo":
			p.switches[a] = true
		case strings.HasPrefix(a, "--"):
			if i+1 < len(args) {
				p.flags[a] = args[i+1]
				i++
			}
		default:
			p.words = append(p.words, a)
		}
	}
	return p
}

func (p parsed) is(words ...string) bool {
	if len(p.words) < len(words) {
		return false
	}
	for i, w := range words {
		if p.words[i] != w {
			return false
		}
	}
	return true
}

const scopeError = "error: your authentication token is missing required scopes [read:project]\nTo request it, run:  gh auth refresh -s read:project"

func answer(st *boardState, args []string) (string, error) {
	p := parse(args)
	if st.ScopeMissing && (p.is("project") || p.is("api")) {
		return "", errors.New(scopeError)
	}
	switch {
	case p.is("project", "list"):
		if err := wantOwner(p); err != nil {
			return "", err
		}
		return fixture("project-list.json")
	case p.is("project", "field-list"):
		if err := wantProject(p); err != nil {
			return "", err
		}
		f, err := fields(st)
		if err != nil {
			return "", err
		}
		b, err := json.Marshal(f)
		return string(b), err
	case p.is("api", "graphql") && strings.Contains(p.fields["query"], "ProjectV2IterationField"):
		if p.fields["id"] != iterationFieldID {
			return "", fmt.Errorf("gh: Could not resolve to a node with the global id of '%s'", p.fields["id"])
		}
		return fixture(st.Iterations)
	case p.is("api", "graphql") && strings.Contains(p.fields["query"], "repository"):
		if p.fields["owner"] != "hyperscaleav" || p.fields["name"] != "omniglass" {
			return "", fmt.Errorf("gh: Could not resolve to a Repository with the name '%s/%s'.", p.fields["owner"], p.fields["name"])
		}
		return issueAnswer(st, p.fields["number"])
	case p.is("project", "item-add"):
		if err := wantProject(p); err != nil {
			return "", err
		}
		n := strings.TrimPrefix(p.flags["--url"], issueURLPrefix)
		is, ok := st.Issues[n]
		if !ok || n == p.flags["--url"] {
			return "", fmt.Errorf("could not resolve %q to an issue", p.flags["--url"])
		}
		if is.Item == nil {
			st.NextItem++
			is.Item = &item{ID: fmt.Sprintf("PVTI_fake%d", st.NextItem)}
		}
		return fmt.Sprintf(`{"id":%q,"title":%q,"type":"Issue","url":%q}`, is.Item.ID, is.Title, p.flags["--url"]), nil
	case p.is("project", "item-edit"):
		return "", edit(st, p)
	case p.is("project", "item-archive"):
		if err := wantProject(p); err != nil {
			return "", err
		}
		it := itemByID(st, p.flags["--id"])
		if it == nil {
			return "", fmt.Errorf("no project item %q", p.flags["--id"])
		}
		it.Archived = !p.switches["--undo"]
		return "{}", nil
	}
	return "", fmt.Errorf("fake gh: unexpected call: %q", args)
}

func wantOwner(p parsed) error {
	if p.flags["--owner"] != "hyperscaleav" || p.flags["--format"] != "json" {
		return fmt.Errorf("fake gh: want --owner hyperscaleav --format json, got %v", p.flags)
	}
	return nil
}

func wantProject(p parsed) error {
	if len(p.words) < 3 || p.words[2] != "1" {
		return fmt.Errorf("fake gh: want project number 1, got %q", p.words)
	}
	return wantOwnerOnly(p)
}

func wantOwnerOnly(p parsed) error {
	if p.flags["--owner"] != "hyperscaleav" {
		return fmt.Errorf("fake gh: want --owner hyperscaleav, got %v", p.flags)
	}
	return nil
}

func fixture(name string) (string, error) {
	b, err := os.ReadFile(filepath.Join(fixtureDir(), name))
	return string(b), err
}

// fixtureDir is testdata/board beside this file. The fake runs with the
// script's working directory, so it finds the fixtures through its own path.
func fixtureDir() string {
	if d := os.Getenv("BOARD_FAKE_FIXTURES"); d != "" {
		return d
	}
	return filepath.Join("testdata", "board")
}

type fieldList struct {
	Fields []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Type    string `json:"type"`
		Options []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"options,omitempty"`
	} `json:"fields"`
	TotalCount int `json:"totalCount"`
}

// fields is the recorded field list, with "In Review" added to Status when the
// test's board has it.
func fields(st *boardState) (*fieldList, error) {
	raw, err := fixture("field-list.json")
	if err != nil {
		return nil, err
	}
	var f fieldList
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		return nil, err
	}
	if st.InReview {
		for i := range f.Fields {
			if f.Fields[i].Name == "Status" {
				opts := f.Fields[i].Options
				done := opts[len(opts)-1]
				f.Fields[i].Options = append(opts[:len(opts)-1:len(opts)-1],
					struct {
						ID   string `json:"id"`
						Name string `json:"name"`
					}{inReviewID, "In Review"}, done)
			}
		}
	}
	return &f, nil
}

func issueAnswer(st *boardState, number string) (string, error) {
	is, ok := st.Issues[number]
	if !ok {
		return "", fmt.Errorf("gh: Could not resolve to an Issue with the number of %s.", number)
	}
	n, _ := strconv.Atoi(number)
	nodes := []any{}
	if it := is.Item; it != nil {
		values := []any{map[string]any{}, map[string]any{}, map[string]any{}}
		for _, v := range []struct{ field, value, key string }{
			{"Priority", it.Priority, "name"},
			{"Status", it.Status, "name"},
			{"Iteration", it.Iteration, "title"},
		} {
			if v.value != "" {
				values = append(values, map[string]any{v.key: v.value, "field": map[string]any{"name": v.field}})
			}
		}
		nodes = append(nodes, map[string]any{
			"id":          it.ID,
			"isArchived":  it.Archived,
			"project":     map[string]any{"id": projectID},
			"fieldValues": map[string]any{"nodes": values},
		})
	}
	b, err := json.Marshal(map[string]any{"data": map[string]any{"repository": map[string]any{"issue": map[string]any{
		"number":       n,
		"title":        is.Title,
		"url":          issueURLPrefix + number,
		"projectItems": map[string]any{"nodes": nodes},
	}}}})
	return string(b), err
}

func itemByID(st *boardState, id string) *item {
	for _, is := range st.Issues {
		if is.Item != nil && is.Item.ID == id {
			return is.Item
		}
	}
	return nil
}

// edit applies an item-edit the way GitHub would: every id must be one the
// project has. It writes any field it is pointed at, Priority included, so the
// never-Priority check sees a write that should not have happened.
func edit(st *boardState, p parsed) error {
	if p.flags["--project-id"] != projectID {
		return fmt.Errorf("gh: Could not resolve to a ProjectV2 with the global id of '%s'", p.flags["--project-id"])
	}
	it := itemByID(st, p.flags["--id"])
	if it == nil {
		return fmt.Errorf("gh: Could not resolve to a ProjectV2Item with the global id of '%s'", p.flags["--id"])
	}
	f, err := fields(st)
	if err != nil {
		return err
	}
	fieldID := p.flags["--field-id"]
	if fieldID == iterationFieldID {
		raw, err := fixture(st.Iterations)
		if err != nil {
			return err
		}
		var its struct {
			Data struct {
				Node struct {
					Configuration struct {
						Iterations []struct{ ID, Title string } `json:"iterations"`
					} `json:"configuration"`
				} `json:"node"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(raw), &its); err != nil {
			return err
		}
		for _, x := range its.Data.Node.Configuration.Iterations {
			if x.ID == p.flags["--iteration-id"] {
				it.Iteration = x.Title
				return nil
			}
		}
		return fmt.Errorf("gh: the iteration %q is not one of the field's", p.flags["--iteration-id"])
	}
	for _, fd := range f.Fields {
		if fd.ID != fieldID {
			continue
		}
		for _, o := range fd.Options {
			if o.ID == p.flags["--single-select-option-id"] {
				switch fd.Name {
				case "Status":
					it.Status = o.Name
				case "Priority":
					it.Priority = o.Name
				}
				return nil
			}
		}
		return fmt.Errorf("gh: the option %q is not one of %s's", p.flags["--single-select-option-id"], fd.Name)
	}
	return fmt.Errorf("gh: Could not resolve to a field with the global id of '%s'", fieldID)
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
	)
	return &board{t: t, dir: dir, env: env}
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
	wantContains(t, "stderr", r.stderr, `no status "Blocked" on Omniglass (Status: Backlog, Todo, In Progress, Done)`)
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
		wantStatusEdit(t, w[0], "PVTI_1145", inReviewID)
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
	wantContains(t, "stderr", r.stderr, `no project named "Nowhere" under hyperscaleav (it has: Applied Monitoring 2026, Omniglass)`)
}

// ---- the fake itself ---------------------------------------------------------

// The fake's issue answer has the shape GitHub's does: issue 58's recorded
// answer (testdata/board/issue-item.json) is what the fake says for issue 58 in
// the state it was recorded in.
func TestFakeAnswersInTheRecordedShape(t *testing.T) {
	st := &boardState{Issues: map[string]*issue{"58": onBoard("ci: codesign + notarize macOS release binaries",
		item{ID: "PVTI_lADOC43oI84BOqlhzgxaTuI", Status: "Todo", Priority: "Medium"})}}
	got, err := issueAnswer(st, "58")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "board", "issue-item.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g, w any
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatal(err)
	}
	gb, _ := json.Marshal(g)
	wb, _ := json.Marshal(w)
	if !bytes.Equal(gb, wb) {
		t.Errorf("fake answer\n%s\nwant the recorded\n%s", gb, wb)
	}
}
