package skills

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

const goodSkill = `---
name: check-thing
description: Check a thing and say what is wrong with it.
category: operate
arguments:
  - name: component
    description: The component to check
    required: true
  - name: hours
    description: How far back to look
---

# Check a thing

Read it with ` + "`tool:get-component`" + `, then ` + "`tool:list-component-alarms`" + `.
Read it again with ` + "`tool:get-component`" + `.
`

func fsOf(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for p, s := range files {
		m[p] = &fstest.MapFile{Data: []byte(s)}
	}
	return m
}

// A skill directory loads into its name, description, category, arguments,
// body and the tools its body names, each once in first-mention order.
func TestLoadReadsASkill(t *testing.T) {
	got, err := Load(fsOf(map[string]string{"check-thing/SKILL.md": goodSkill}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d skills, want 1", len(got))
	}
	s := got[0]
	if s.Name != "check-thing" || s.Category != "operate" || !strings.HasPrefix(s.Description, "Check a thing") {
		t.Errorf("front matter = %+v", s)
	}
	want := []Argument{{Name: "component", Description: "The component to check", Required: true}, {Name: "hours", Description: "How far back to look"}}
	if !reflect.DeepEqual(s.Arguments, want) {
		t.Errorf("arguments = %+v, want %+v", s.Arguments, want)
	}
	if !reflect.DeepEqual(s.Tools, []string{"get-component", "list-component-alarms"}) {
		t.Errorf("tools = %v", s.Tools)
	}
	if string(s.Raw) != goodSkill {
		t.Errorf("raw is not the file's bytes")
	}
	if !strings.HasPrefix(s.Body, "# Check a thing") {
		t.Errorf("body = %q, want the markdown after the front matter", s.Body)
	}
}

// Every way a skill file can be wrong fails the load, naming the file.
func TestLoadRefusesABadSkill(t *testing.T) {
	cases := map[string]string{
		"name differs from its directory": strings.Replace(goodSkill, "name: check-thing", "name: other-thing", 1),
		"an unknown front matter key":     strings.Replace(goodSkill, "category: operate", "category: operate\ntools: [get-component]", 1),
		"an unknown category":             strings.Replace(goodSkill, "category: operate", "category: tinker", 1),
		"no description":                  strings.Replace(goodSkill, "description: Check a thing and say what is wrong with it.\n", "", 1),
		"no front matter":                 "# Check a thing\n",
		"an unnamed argument":             strings.Replace(goodSkill, "name: hours", "name: \"\"", 1),
		"an argument named twice":         strings.Replace(goodSkill, "name: hours", "name: component", 1),
		"no tool named":                   strings.ReplaceAll(goodSkill, "`tool:", "`"),
	}
	for why, body := range cases {
		t.Run(why, func(t *testing.T) {
			_, err := Load(fsOf(map[string]string{"check-thing/SKILL.md": body}))
			if err == nil {
				t.Fatalf("load succeeded, want a refusal")
			}
			if !strings.Contains(err.Error(), "check-thing") {
				t.Errorf("error %q does not name the skill", err)
			}
		})
	}
}

// Skills come back sorted by name, whatever order the directory walk gives.
func TestLoadSortsByName(t *testing.T) {
	other := strings.Replace(goodSkill, "name: check-thing", "name: alpha-thing", 1)
	got, err := Load(fsOf(map[string]string{"check-thing/SKILL.md": goodSkill, "alpha-thing/SKILL.md": other}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got[0].Name != "alpha-thing" || got[1].Name != "check-thing" {
		t.Errorf("order = %s, %s", got[0].Name, got[1].Name)
	}
}

// The prompt is the body, then the arguments the client gave, in declared
// order; a missing required argument is refused.
func TestPromptCarriesTheArguments(t *testing.T) {
	sk, err := Load(fsOf(map[string]string{"check-thing/SKILL.md": goodSkill}))
	if err != nil {
		t.Fatal(err)
	}
	s := sk[0]
	got, err := s.Prompt(map[string]string{"hours": "6", "component": "panel-1"})
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if !strings.HasPrefix(got, s.Body) {
		t.Errorf("prompt does not start with the body")
	}
	tail := strings.TrimPrefix(got, s.Body)
	if !strings.Contains(tail, "- component: panel-1\n- hours: 6") {
		t.Errorf("prompt tail = %q, want the arguments in declared order", tail)
	}
	if bare, err := s.Prompt(map[string]string{"component": "panel-1"}); err != nil || strings.Contains(bare, "hours") {
		t.Errorf("an unset optional argument is listed: %q %v", bare, err)
	}
	if _, err := s.Prompt(nil); err == nil || !strings.Contains(err.Error(), "component") {
		t.Errorf("a missing required argument = %v, want a refusal naming it", err)
	}
	if _, err := s.Prompt(map[string]string{"component": "p", "colour": "red"}); err == nil || !strings.Contains(err.Error(), "colour") {
		t.Errorf("an undeclared argument = %v, want a refusal naming it", err)
	}
}

// The embedded registry loads: a broken skill file fails here, in make test.
func TestTheEmbeddedSkillsLoad(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Fatal("no skills embedded")
	}
	for _, s := range all {
		got, ok := Get(s.Name)
		if !ok || got.Name != s.Name {
			t.Errorf("Get(%q) = %v", s.Name, ok)
		}
	}
	if _, ok := Get("no-such-skill"); ok {
		t.Error("Get found a skill that does not exist")
	}
}
