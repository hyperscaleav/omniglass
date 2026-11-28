package devseed_test

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/seed"
)

// A shot or walkthrough that names a component by its label waits for exactly
// that label, so one the fleet no longer shows fails at capture, minutes into a
// lab (#2847: #2808 stopped numbering a slot of one, and a dozen references
// still waited for "Video Bar 1" or "Touch Panel 2"). These tests fail it at
// test time: every component label a script names must be one its fleet shows.
//
// A label here is a component type's shipped label, bare or with an ordinal
// ("Video Bar", "Touch Panel 2"). The labels a fleet shows are read from the
// fleet itself, not listed: the dev fleet's from a seeded database, the
// corporate scenario's from its resolved plan. A script is checked for a quoted
// string that is exactly a label, and an unquoted "<type label> <n>", on any
// line that is script (a selector, an expectation, a value); a caption, alt or
// description and a comment are prose, and recordings carry their captions, so
// they are neither checked nor used to find the room a script runs in.
//
// A script that names a room, a system or a component (a /systems/<name> path,
// a {...$comp.<name>} address, a quoted room label) is held to the labels those
// show, so "Touch Panel 1" does not pass because some other room has one. A
// script that names none is held to the fleet. A room or system is recognised
// by its name, outside a caption, alt or description line.
const repoRoot = "../.."

var proseLine = regexp.MustCompile(`^\s*(?:#|(?:-\s*)?(?:caption|alt|description|title)\s*:)`)

func componentLabelPattern() *regexp.Regexp {
	var types []string
	for _, l := range seed.ShippedComponentTypeLabels() {
		types = append(types, regexp.QuoteMeta(l))
	}
	sort.Slice(types, func(i, j int) bool { return len(types[i]) > len(types[j]) })
	t := `(?:` + strings.Join(types, "|") + `)`
	// 2: a quoted string that is only a label; 3: an unquoted numbered label.
	return regexp.MustCompile(`(['"])(` + t + `(?: \d+)?)['"]|\b(` + t + ` \d+)\b`)
}

// namedLabels is each component label a text names, with its first line.
func namedLabels(re *regexp.Regexp, text string) map[string]int {
	out := map[string]int{}
	for i, line := range strings.Split(text, "\n") {
		if proseLine.MatchString(line) {
			continue
		}
		for _, m := range re.FindAllStringSubmatch(line, -1) {
			l := m[2]
			if l == "" {
				l = m[3]
			}
			if _, ok := out[l]; !ok {
				out[l] = i + 1
			}
		}
	}
	return out
}

var slugBreak = regexp.MustCompile(`[^a-z0-9]+`)

// scopeTokens is every name in a text a room, system or component could be
// found by: its words (outside prose lines) as they are written and run
// together with hyphens, so "Hq Annex 1 03" reads as hq-annex-1-03.
func scopeTokens(text string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		if proseLine.MatchString(line) {
			continue
		}
		low := strings.ToLower(line)
		for _, tok := range regexp.MustCompile(`[a-z0-9][a-z0-9-]*`).FindAllString(low, -1) {
			out[tok] = true
		}
		for _, q := range regexp.MustCompile(`['"]([^'"]+)['"]`).FindAllStringSubmatch(low, -1) {
			out[strings.Trim(slugBreak.ReplaceAllString(q[1], "-"), "-")] = true
		}
	}
	return out
}

// fleetLabels is what a fleet shows: its labels, and by each room, system and
// component name the labels found under it.
type fleetLabels struct {
	all     map[string]bool
	byScope map[string]map[string]bool
}

func newFleetLabels() *fleetLabels {
	return &fleetLabels{all: map[string]bool{}, byScope: map[string]map[string]bool{}}
}

func (f *fleetLabels) add(label string, scopes ...string) {
	f.all[label] = true
	for _, s := range scopes {
		if s == "" {
			continue
		}
		if f.byScope[s] == nil {
			f.byScope[s] = map[string]bool{}
		}
		f.byScope[s][label] = true
	}
}

// shows is whether the labels a text's scopes show include label; a text that
// names no scope the fleet knows is held to the fleet.
func (f *fleetLabels) shows(label string, tokens map[string]bool) bool {
	scoped := false
	for tok := range tokens {
		if set, ok := f.byScope[tok]; ok {
			scoped = true
			if set[label] {
				return true
			}
		}
	}
	return !scoped && f.all[label]
}

func checkScript(t *testing.T, re *regexp.Regexp, fleet string, f *fleetLabels, path, text string) int {
	t.Helper()
	tokens := scopeTokens(text)
	n := 0
	for label, line := range namedLabels(re, text) {
		n++
		if !f.shows(label, tokens) {
			t.Errorf("%s:%d names the component label %q, which the %s fleet does not show there (a line number is within the script)", path, line, label, fleet)
		}
	}
	return n
}

func repoFiles(t *testing.T, glob string) map[string]string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(repoRoot, glob))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no files match %s: %v", glob, err)
	}
	out := map[string]string{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		out[filepath.ToSlash(strings.TrimPrefix(p, repoRoot+"/"))] = string(b)
	}
	return out
}

// frontmatter is a docs page's leading yaml block, where its shots are
// declared; the page's prose may name a component any way it likes.
func frontmatter(text string) string {
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return ""
	}
	fm, _, _ := strings.Cut(rest, "\n---")
	return fm
}

// The walkthroughs and the docs shots run on the dev fleet.
func TestDevFleetScriptsNameOnlyLabelsTheFleetShows(t *testing.T) {
	t.Parallel()
	ctx, conn, _ := seededFleet(t)
	rows, err := conn.Query(context.Background(), `
		select c.label, coalesce(l.name, ''), coalesce(s.name, ''), c.name
		from component c
		left join location l on l.id = c.location_id
		left join system_slot_assignment a on a.component_id = c.id
		left join system s on s.id = a.system_id
		where c.label is not null`)
	_ = ctx
	if err != nil {
		t.Fatal(err)
	}
	f := newFleetLabels()
	for rows.Next() {
		var label, room, system, name string
		if err := rows.Scan(&label, &room, &system, &name); err != nil {
			t.Fatal(err)
		}
		f.add(label, room, system)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	re := componentLabelPattern()
	files := repoFiles(t, "web/e2e/walkthroughs/*.yaml")
	for p, text := range repoFiles(t, "docs/src/content/docs/*/*.md*") {
		files[p] = frontmatter(text)
	}
	checked := 0
	for p, text := range files {
		checked += checkScript(t, re, "dev", f, p, text)
	}
	if checked == 0 {
		t.Fatal("no component label was named in any dev script: the guard reads nothing")
	}
}

// The marketing shots run on the scenario their shot list names; each shot is
// held to the labels of the rooms it names.
func TestMarketingShotsNameOnlyLabelsTheirFleetShows(t *testing.T) {
	t.Parallel()
	text := repoFiles(t, "web/e2e/marketing/shots.yaml")["web/e2e/marketing/shots.yaml"]
	scenario := regexp.MustCompile(`(?m)^lab:\n\s+scenario:\s*(\S+)\n\s+systems:\s*(\d+)`).FindStringSubmatch(text)
	if scenario == nil || scenario[1] != "corporate" {
		t.Fatalf("the shot list's lab is not the corporate scenario at a size: %v", scenario)
	}
	size, _ := strconv.Atoi(scenario[2])
	f := newFleetLabels()
	for _, c := range devseed.PlannedComponentLabels(corporatePlan(t, size)) {
		f.add(c.Label, c.Room, c.System, c.Component)
	}
	re := componentLabelPattern()
	head, shots, ok := strings.Cut(text, "\nshots:\n")
	if !ok {
		t.Fatal("the shot list has no shots: section")
	}
	offset := strings.Count(head, "\n") + 2
	checked := 0
	starts := regexp.MustCompile(`(?m)^  - id: (\S+)`).FindAllStringSubmatchIndex(shots, -1)
	for i, m := range starts {
		end := len(shots)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		id := shots[m[2]:m[3]]
		checked += checkScript(t, re, "corporate", f, "shot "+id+" (web/e2e/marketing/shots.yaml, from line "+strconv.Itoa(offset+strings.Count(shots[:m[0]], "\n"))+")", shots[m[0]:end])
	}
	if checked == 0 {
		t.Fatal("no component label was named in any shot: the guard reads nothing")
	}
}
