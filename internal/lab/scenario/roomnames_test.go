package scenario

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// The old "Adjective Surname" generator's adjectives, which no room name may
// carry now that a company's rooms are named the way a company names them.
var retiredAdjectives = []string{"admiring", "affectionate", "agitated", "angry", "boring", "brave", "busy", "clever", "cranky", "dreamy", "eager", "elegant", "festive", "focused", "friendly", "funny", "goofy", "happy", "hungry", "jolly", "keen", "kind", "lucid", "modest", "nervous", "nice", "pensive", "quirky", "relaxed", "serene", "sleepy", "stoic", "thirsty", "trusting", "vibrant", "zen"}

func TestRoomThemesHoldEachWordOnceAndNoAdjective(t *testing.T) {
	for name, words := range roomThemes {
		seen := map[string]bool{}
		for _, w := range words {
			if seen[w] {
				t.Errorf("theme %s holds %q twice", name, w)
			}
			seen[w] = true
			for _, a := range retiredAdjectives {
				if strings.EqualFold(w, a) {
					t.Errorf("theme %s holds the adjective %q", name, w)
				}
			}
			if w == "" || w != strings.TrimSpace(w) {
				t.Errorf("theme %s holds the word %q", name, w)
			}
		}
		if len(words) < 50 {
			t.Errorf("theme %s holds %d words; a site would run it dry", name, len(words))
		}
	}
}

// A room is named as a company names it: the site's one theme for the rooms
// that are no function, the function's name numbered for the rest, each unique
// within its site and the same on every resolve.
func TestResolveNamesRoomsByASiteThemeAndByFunction(t *testing.T) {
	m, cat := corporate(t)
	p, err := Resolve(m, cat, 1000)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Resolve(m, cat, 1000)
	if err != nil {
		t.Fatal(err)
	}
	function := regexp.MustCompile(`^(Huddle|Auditorium|Signage)( [0-9]+)?$`)
	for si, sp := range p.Sites {
		seen := map[string]bool{}
		themed := map[string]bool{}
		for i, s := range sp.Systems {
			if s.Label != again.Sites[si].Systems[i].Label {
				t.Fatalf("%s: the label is %q then %q for one plan", s.Name, s.Label, again.Sites[si].Systems[i].Label)
			}
			if seen[s.Label] {
				t.Fatalf("site %s names two rooms %q", sp.Name, s.Label)
			}
			seen[s.Label] = true
			if function.MatchString(s.Label) {
				continue
			}
			base := strings.TrimRight(s.Label, " 0123456789")
			themed[base] = true
			for _, a := range retiredAdjectives {
				if strings.Contains(strings.ToLower(s.Label), a+" ") {
					t.Fatalf("%s is labeled %q, which carries the adjective %q", s.Name, s.Label, a)
				}
			}
		}
		// One theme a site: every themed room's word is in a single list.
		inOne := false
		for _, words := range roomThemes {
			set := map[string]bool{}
			for _, w := range words {
				set[w] = true
			}
			all := true
			for w := range themed {
				all = all && set[w]
			}
			inOne = inOne || all
		}
		if len(themed) > 0 && !inOne {
			t.Errorf("site %s mixes themes: %v", sp.Name, themed)
		}
	}
	// The headquarters is named after scientists.
	hq := p.Sites[0]
	var got string
	for _, s := range hq.Systems {
		if s.Standard == "mr65" {
			got = s.Label
			break
		}
	}
	found := false
	for _, w := range roomThemes["scientists"] {
		if strings.HasPrefix(got, w) {
			found = true
		}
	}
	if !found {
		t.Errorf("the headquarters' first meeting room is %q, not a scientist", got)
	}
}

func TestFunctionRoomsAreNumberedAndALoneOneIsNot(t *testing.T) {
	if got := function("Auditorium", 1, 1); got != "Auditorium" {
		t.Errorf("a lone auditorium reads %q", got)
	}
	if got := function("Huddle", 3, 12); got != "Huddle 3" {
		t.Errorf("the third of twelve huddle rooms reads %q", got)
	}
}

func TestAThemeRunDryTakesItsWordsAgainNumbered(t *testing.T) {
	r := newRoomNamer("s", "site", "lakes")
	n := len(roomThemes["lakes"])
	seen := map[string]bool{}
	for k := 1; k <= 2*n; k++ {
		name := r.themed("mr65", k)
		if seen[name] {
			t.Fatalf("the %d-th room reads %q twice", k, name)
		}
		seen[name] = true
	}
	if !seen[fmt.Sprintf("%s 2", roomThemes["lakes"][0])] && len(seen) != 2*n {
		t.Fatalf("a second pass should number the words again; got %d names", len(seen))
	}
}

func TestValidateRefusesAnUnknownRoomTheme(t *testing.T) {
	m, cat := corporate(t)
	m.Archetypes[0].RoomNames = []Ref{{Name: "adjectives", Line: 9}}
	if err := Validate(m, cat); err == nil || !strings.Contains(err.Error(), `room theme "adjectives"`) {
		t.Fatalf("an unknown theme was accepted: %v", err)
	}
}
