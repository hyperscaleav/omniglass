package vocab

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/health"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func TestTableLoads(t *testing.T) {
	v, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Terms) == 0 {
		t.Fatal("no terms")
	}
}

func TestValidateRefusesBadRows(t *testing.T) {
	cases := map[string]string{
		"duplicate word": `terms:
  - {word: a, plural: as, definition: An a., audience: operator}
  - {word: a, plural: as, definition: An a., audience: admin}`,
		"duplicate collection": `terms:
  - {word: a, plural: as, definition: An a., audience: operator, collection: x}
  - {word: b, plural: bs, definition: A b., audience: operator, collection: x}`,
		"collection both term and not_term": `terms:
  - {word: a, plural: as, definition: An a., audience: operator, collection: x}
not_terms:
  - {collection: x, reason: because}`,
		"bad audience":       `terms: [{word: a, plural: as, definition: An a., audience: everyone}]`,
		"no period":          `terms: [{word: a, plural: as, definition: An a, audience: operator}]`,
		"no definition":      `terms: [{word: a, plural: as, audience: operator}]`,
		"label without nav":  `terms: [{word: a, plural: as, definition: An a., audience: operator, nav_label: A}]`,
		"bad pattern":        `terms: [{word: a, plural: as, definition: An a., audience: operator, retired: [{word: b, pattern: "(", origin: "#1"}]}]`,
		"bad allow":          `terms: [{word: a, plural: as, definition: An a., audience: operator, retired: [{word: b, pattern: b, origin: "#1", allow: "("}]}]`,
		"retired no origin":  `terms: [{word: a, plural: as, definition: An a., audience: operator, retired: [{word: b, pattern: b}]}]`,
		"unknown field":      `terms: [{word: a, plural: as, definition: An a., audience: operator, colour: red}]`,
		"not_term no reason": `not_terms: [{nav: /x}]`,
		"not_term both keys": `not_terms: [{nav: /x, collection: x, reason: r}]`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parse([]byte(src)); err == nil {
				t.Fatal("parsed, want an error")
			}
		})
	}
}

func TestLabelDefaultsToCapitalizedPlural(t *testing.T) {
	if got := (Term{Plural: "component types"}).Label(); got != "Component types" {
		t.Fatalf("Label() = %q", got)
	}
	if got := (Term{Plural: "metric types", NavLabel: "Metrics"}).Label(); got != "Metrics" {
		t.Fatalf("Label() = %q", got)
	}
}

// navEntry matches one console nav entry in web/src/lib/nav.ts: rail entries
// list label before path, off-rail entries path before label.
var navEntry = regexp.MustCompile(`\{\s*(?:label: "([^"]+)", path: "([^"]+)"|path: "([^"]+)", label: "([^"]+)")`)

func consoleNav(t *testing.T) map[string]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "web", "src", "lib", "nav.ts"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, m := range navEntry.FindAllStringSubmatch(string(b), -1) {
		if m[2] != "" {
			out[m[2]] = m[1]
		} else {
			out[m[3]] = m[4]
		}
	}
	if len(out) < 30 {
		t.Fatalf("parsed only %d nav entries from nav.ts; has its shape changed?", len(out))
	}
	return out
}

// Every console nav entry names a term (or is a listed not_term), and its
// label is that term's word.
func TestConsoleNavSpeaksTheVocabulary(t *testing.T) {
	nav := MustLoad().Nav()
	console := consoleNav(t)
	for path, label := range console {
		term, ok := nav[path]
		if !ok {
			t.Errorf("nav %s (%q) maps to no term: add it to internal/vocab/terms.yaml, or to not_terms with why", path, label)
			continue
		}
		if term != nil && term.Label() != label {
			t.Errorf("nav %s is labelled %q, but its term %q says %q", path, label, term.Word, term.Label())
		}
	}
	for path := range nav {
		if _, ok := console[path]; !ok {
			t.Errorf("terms.yaml names nav %s, which the console no longer has", path)
		}
	}
}

// Every health verdict value is a term.
func TestEveryVerdictIsATerm(t *testing.T) {
	verdicts := MustLoad().Verdicts()
	for v := health.Healthy; v <= health.Outage; v++ {
		if _, ok := verdicts[v.String()]; !ok {
			t.Errorf("verdict %q has no term", v)
		}
	}
	if len(verdicts) != int(health.Outage)+1 {
		t.Errorf("terms.yaml names %d verdicts, health has %d", len(verdicts), int(health.Outage)+1)
	}
}

// The committed glossary is what the table renders.
func TestGlossaryIsGenerated(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), GlossaryPath))
	if err != nil {
		t.Fatalf("%v (run make gen)", err)
	}
	if string(b) != MustLoad().RenderGlossary() {
		t.Fatalf("%s is stale: run make gen", GlossaryPath)
	}
}

func TestGlossaryListsEveryTermOnce(t *testing.T) {
	v := MustLoad()
	g := v.RenderGlossary()
	for _, term := range v.Terms {
		if n := strings.Count(g, "| **"+term.Word+"** |"); n != 1 {
			t.Errorf("term %q appears %d times in the glossary", term.Word, n)
		}
	}
}

func TestBannedCarriesEveryRetiredWord(t *testing.T) {
	v := MustLoad()
	n := 0
	for _, t := range v.Terms {
		n += len(t.Retired)
	}
	if got := len(v.Banned()); got != n || n == 0 {
		t.Fatalf("Banned() = %d entries, want %d", got, n)
	}
	for _, b := range v.Banned() {
		if b.Pattern.MatchString(b.Replacement) {
			t.Errorf("retired pattern %s matches its own replacement %q", b.Pattern, b.Replacement)
		}
	}
}
