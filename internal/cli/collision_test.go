package cli

import (
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Two commands registered under one parent with the same name is not an error in
// cobra: both are added, and lookup returns whichever it finds first. The second
// is unreachable, silently, and the only symptom is a command that runs the wrong
// thing. That has now happened three times (the `members` collision under the
// principal groups, `type list`, and the `node` run mode swallowing every
// generated node command), each found by a person typing it rather than by the
// build.
//
// The hand-written run modes and the generated API groups compose on one root, so
// nothing owns the whole namespace and no single file can be reviewed to prevent
// this. The tree is the only place the two sets meet, so it is the only place the
// check can live.
func TestNoCommandNameCollisions(t *testing.T) {
	var collisions []string
	var walk func(parent *cobra.Command, path string)
	walk = func(parent *cobra.Command, path string) {
		seen := map[string][]string{}
		for _, c := range parent.Commands() {
			// A name and its aliases occupy the same namespace, since either
			// resolves to the command.
			for _, n := range append([]string{c.Name()}, c.Aliases...) {
				seen[n] = append(seen[n], c.Short)
			}
		}
		names := make([]string, 0, len(seen))
		for n := range seen {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			if len(seen[n]) > 1 {
				collisions = append(collisions,
					strings.TrimSpace(path+" "+n)+" is registered "+
						itoa(len(seen[n]))+" times: "+strings.Join(seen[n], " | "))
			}
		}
		for _, c := range parent.Commands() {
			walk(c, strings.TrimSpace(path+" "+c.Name()))
		}
	}
	walk(Root("test"), "omniglass")

	var unexpected []string
	for _, c := range collisions {
		name := c[:strings.Index(c, " is registered ")]
		if _, known := knownCollisions[name]; !known {
			unexpected = append(unexpected, c)
		}
	}
	if len(unexpected) > 0 {
		t.Errorf("%d NEW shadowed command(s); the second of each is unreachable:\n  %s",
			len(unexpected), strings.Join(unexpected, "\n  "))
	}

	// The list may only shrink. A fixed collision that is not removed from it
	// would leave the guard blind to that name regressing later.
	for name, issue := range knownCollisions {
		found := false
		for _, c := range collisions {
			if strings.HasPrefix(c, name+" is registered ") {
				found = true
			}
		}
		if !found {
			t.Errorf("%q is listed as a known collision (%s) but no longer collides; "+
				"delete the entry, or the guard stops watching that name", name, issue)
		}
	}
}

// knownCollisions are the shadowed commands that exist today, each tracked and
// each needing a naming decision rather than a mechanical fix (renaming them
// changes the documented CLI surface). This list may only shrink: the guard
// fails on anything NOT in it, so a new collision cannot land, and it fails on
// an entry that stops colliding, so a fix must delete its entry rather than
// leave the name unwatched.
//
// The root cause is one thing: cligen's commandWords derives the group from a
// single path segment, so two different parents ending in the same collection
// noun collapse into one group.
var knownCollisions = map[string]string{
	"omniglass grant create": "#357 (principal grants unreachable; the group variants win)",
	"omniglass grant delete": "#357",
	"omniglass avatar list":  "#357 (self vs a principal's, distinguished only by arity)",
	"omniglass type list":    "#319 (location types vs secret types)",
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return "many"
}

// TestNodeRunIsReachable pins the outcome, not the wiring: the edge run mode is
// attached to the generated `node` group after generation, so if the API ever
// stopped exposing node routes the group would vanish and the mode would go with
// it, silently. Asserting both halves resolve is what catches that, and it is the
// regression for the collision itself: before this, `node list` resolved to the
// daemon and failed asking for --token.
func TestNodeRunIsReachable(t *testing.T) {
	root := Root("test")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"node", "run"}, "run"},
		{[]string{"node", "list"}, "list"},
		{[]string{"node", "enroll"}, "enroll"},
	} {
		cmd, _, err := root.Find(tc.args)
		if err != nil {
			t.Errorf("find %v: %v", tc.args, err)
			continue
		}
		if cmd.Name() != tc.want {
			t.Errorf("%v resolved to %q, want %q", tc.args, cmd.Name(), tc.want)
		}
	}
}
