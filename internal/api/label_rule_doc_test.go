package api_test

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	labelengine "github.com/hyperscaleav/omniglass/internal/label"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A `label_rule` field's description teaches the rule language on the surface an
// operator actually meets it: it is the help text in the console's field, the
// row in the generated CLI reference, and the schema every typed client carries.
// Two of those descriptions ENUMERATE the closed data map and the closed function
// set, and both sets are declared in code (#729, #701).
//
// So the enumeration is a hand-written copy of a generated fact, which is the
// drift this repo's generate-first rule exists to stop, and it had already
// drifted: the key list was written before #685 added the placement facts, so it
// omitted LocationLabel and SystemTypeLabel, and the docs page beside it listed
// them. A Huma description is a struct tag, so it cannot be built from the
// declaration at run time; it is held to the declaration here instead.

// factsList matches the parenthesised key enumeration in a label_rule description.
var factsList = regexp.MustCompile(`facts \(([^)]*)\)`)

// funcsList matches the function enumeration ("the functions title, upper, lower,
// slug and words").
var funcsList = regexp.MustCompile(`the functions ([a-z]+(?:, [a-z]+)*(?: and [a-z]+)?)`)

func TestLabelRuleDescriptionsNameTheDeclaredKeysAndFunctions(t *testing.T) {
	raw, err := os.ReadFile("../../api/openapi.json")
	if err != nil {
		t.Fatalf("read openapi.json (run make gen): %v", err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse openapi.json: %v", err)
	}

	checked := 0
	for name, sc := range doc.Components.Schemas {
		p, ok := sc.Properties["label_rule"]
		if !ok || !strings.Contains(p.Description, "facts (") {
			continue
		}
		checked++
		want := strings.Join(storage.LabelDataKeys("component"), ", ")
		m := factsList.FindStringSubmatch(p.Description)
		if m == nil {
			t.Errorf("%s.label_rule names a fact list this test cannot read: %q", name, p.Description)
			continue
		}
		if m[1] != want {
			t.Errorf("%s.label_rule enumerates the data map as (%s); the declaration is (%s)", name, m[1], want)
		}
		fm := funcsList.FindStringSubmatch(p.Description)
		if fm == nil {
			t.Errorf("%s.label_rule names no function list: %q", name, p.Description)
			continue
		}
		if got := splitList(fm[1]); !equal(got, labelengine.FuncNames()) {
			t.Errorf("%s.label_rule names the functions %v; the engine's set is %v", name, got, labelengine.FuncNames())
		}
	}
	if checked == 0 {
		t.Fatal("no label_rule description enumerates the data map; this guard is checking nothing")
	}
}

// splitList reads "a, b, c and d" as its members.
func splitList(s string) []string {
	s = strings.ReplaceAll(s, " and ", ", ")
	var out []string
	for _, p := range strings.Split(s, ", ") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
