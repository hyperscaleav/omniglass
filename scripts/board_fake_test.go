// The fake gh the board.sh tests drive the script against: the board's state,
// kept in a file between runs, and the answers GitHub would give for it, read
// from the recorded fixtures in testdata/board.
package scripts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// ---- the board's state -----------------------------------------------------

type boardState struct {
	InReview     bool              `json:"in_review"`     // the Status field has an "In Review" option
	ScopeMissing bool              `json:"scope_missing"` // the token lacks the project scope
	Iterations   string            `json:"iterations"`    // the iterations fixture the board answers with
	Issues       map[string]*issue `json:"issues"`        // the repo's issues, by number
	NextItem     int               `json:"next_item"`
	// The board's "Item added to project" workflow: the Status it gives a new
	// item, and how many reads of the item still see none before it lands.
	AddedStatus     string `json:"added_status"`
	AddedAfterReads int    `json:"added_after_reads"`
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
	// A Status the board's workflow has yet to write, and the reads before it does.
	Pending      string `json:"pending"`
	PendingReads int    `json:"pending_reads"`
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
			is.Item = &item{ID: fmt.Sprintf("PVTI_fake%d", st.NextItem), Pending: st.AddedStatus, PendingReads: st.AddedAfterReads}
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
		ID      string   `json:"id"`
		Name    string   `json:"name"`
		Type    string   `json:"type"`
		Options []option `json:"options,omitempty"`
	} `json:"fields"`
	TotalCount int `json:"totalCount"`
}

type option struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// fields is the recorded field list as a test's board has it: its Status field
// holds an "In Review" option exactly when st.InReview says so, whether or not
// the recording does. The recording's own option is kept when it has one; else
// one is added before Done.
func fields(st *boardState) (*fieldList, error) {
	raw, err := fixture("field-list.json")
	if err != nil {
		return nil, err
	}
	var f fieldList
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		return nil, err
	}
	for i := range f.Fields {
		if f.Fields[i].Name != "Status" {
			continue
		}
		var opts []option
		has := false
		for _, o := range f.Fields[i].Options {
			if strings.EqualFold(o.Name, "In Review") {
				has = true
				if !st.InReview {
					continue
				}
			}
			opts = append(opts, o)
		}
		if st.InReview && !has {
			at := len(opts)
			for j, o := range opts {
				if o.Name == "Done" {
					at = j
				}
			}
			opts = append(opts[:at:at], append([]option{{injectedInReviewID, "In Review"}}, opts[at:]...)...)
		}
		f.Fields[i].Options = opts
	}
	return &f, nil
}

func (f *fieldList) fieldID(name string) string {
	for _, fd := range f.Fields {
		if fd.Name == name {
			return fd.ID
		}
	}
	return ""
}

func (f *fieldList) optionID(field, name string) string {
	for _, fd := range f.Fields {
		for _, o := range fd.Options {
			if fd.Name == field && o.Name == name {
				return o.ID
			}
		}
	}
	return ""
}

// statusNames is the board's Status options, in order, as a test board has them.
func statusNames(t *testing.T, st boardState) string {
	t.Helper()
	f, err := fields(&st)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, fd := range f.Fields {
		for _, o := range fd.Options {
			if fd.Name == "Status" {
				names = append(names, o.Name)
			}
		}
	}
	return strings.Join(names, ", ")
}

func issueAnswer(st *boardState, number string) (string, error) {
	is, ok := st.Issues[number]
	if !ok {
		return "", fmt.Errorf("gh: Could not resolve to an Issue with the number of %s.", number)
	}
	n, _ := strconv.Atoi(number)
	nodes := []any{}
	if it := is.Item; it != nil {
		if it.Pending != "" {
			if it.PendingReads == 0 {
				it.Status, it.Pending = it.Pending, ""
			} else {
				it.PendingReads--
			}
		}
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
