package label_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/label"
)

// The rule engine (#682). Two failure modes matter more than any nicety here,
// and they are what most of this file is about: a rule that cannot PARSE must
// be refused before it is ever stored, and a rule that fails to EXECUTE must
// degrade rather than propagate, because the thing it is rendering is read far
// more often than it is written.

func TestParseAcceptsAWellFormedRule(t *testing.T) {
	for _, src := range []string{
		"",
		"Ceiling Mic",
		"{{.TypeName}}",
		"{{.TypeName}}{{if .Ordinal}} {{.Ordinal}}{{end}}",
		"{{.TypeName | title}} {{.Stem | upper}} {{.ProductName | lower}} {{.VendorName | slug}}",
	} {
		if _, err := label.Basic.Parse(src); err != nil {
			t.Fatalf("Parse(%q) = %v, want no error", src, err)
		}
	}
}

// An unparseable rule is refused HERE, at the one place a rule enters the
// system, so a broken template can never reach the column that a write path
// will later execute. Each of these is a distinct way to be unparseable: an
// unclosed action, an unbalanced block, and a function the closed FuncMap does
// not define.
func TestParseRefusesAnUnparseableRule(t *testing.T) {
	for _, src := range []string{
		"{{.TypeName",
		"{{if .Ordinal}}{{.Ordinal}}",
		"{{.TypeName | exec}}",
		"{{.TypeName | env}}",
	} {
		_, err := label.Basic.Parse(src)
		if err == nil {
			t.Fatalf("Parse(%q) = nil error, want a refusal", src)
		}
		if !errors.Is(err, label.ErrRuleUnparseable) {
			t.Fatalf("Parse(%q) = %v, want ErrRuleUnparseable", src, err)
		}
	}
}

// The FuncMap is CLOSED: exactly title, upper, lower, slug. A function name
// outside that set fails at parse time (above), and this pins the set itself so
// growing it is a deliberate act with a test to change, not a drive-by.
func TestFuncMapIsExactlyTheClosedSet(t *testing.T) {
	want := []string{"title", "upper", "lower", "slug"}
	got := label.FuncNames()
	if len(got) != len(want) {
		t.Fatalf("FuncNames() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("FuncNames() = %v, want %v", got, want)
		}
	}
}

func TestRenderSubstitutesTheDataMap(t *testing.T) {
	r := mustParse(t, "{{.TypeName}} {{.Ordinal}}")
	got, err := r.Render(label.Data{"TypeName": "Ceiling Mic", "Ordinal": "2"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != "Ceiling Mic 2" {
		t.Fatalf("Render = %q, want %q", got, "Ceiling Mic 2")
	}
}

// An absent fact is the EMPTY STRING, not a nil and not "<no value>". That is
// the whole reason Data is map[string]string rather than map[string]any: a
// missing key renders as nothing an operator has to explain, and it is falsy,
// so {{if}} works on it.
func TestRenderTreatsAnAbsentFactAsEmptyRatherThanNil(t *testing.T) {
	r := mustParse(t, "[{{.Ordinal}}][{{.NeverDefined}}]")
	got, err := r.Render(label.Data{"TypeName": "Ceiling Mic"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != "[][]" {
		t.Fatalf("Render = %q, want %q", got, "[][]")
	}
}

func TestRenderConditionalOnAnAbsentFact(t *testing.T) {
	r := mustParse(t, "{{.TypeName}}{{if .Ordinal}} {{.Ordinal}}{{end}}")
	with, err := r.Render(label.Data{"TypeName": "Ceiling Mic", "Ordinal": "2"})
	if err != nil {
		t.Fatalf("Render with ordinal: %v", err)
	}
	if with != "Ceiling Mic 2" {
		t.Fatalf("with ordinal = %q, want %q", with, "Ceiling Mic 2")
	}
	without, err := r.Render(label.Data{"TypeName": "Ceiling Mic"})
	if err != nil {
		t.Fatalf("Render without ordinal: %v", err)
	}
	if without != "Ceiling Mic" {
		t.Fatalf("without ordinal = %q, want %q", without, "Ceiling Mic")
	}
}

// Whitespace is normalized so the unconditional form of a rule stays usable
// when one of its facts is absent: "{{.A}} {{.B}}" with B empty is "A", not
// "A ". Without this every rule would have to be written with {{if}} guards
// around every separator, which is a lot of syntax to demand for the common
// case.
func TestRenderCollapsesWhitespaceAndTrims(t *testing.T) {
	r := mustParse(t, "  {{.A}}   {{.B}} {{.C}}  ")
	got, err := r.Render(label.Data{"A": "Ceiling", "C": "Mic"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != "Ceiling Mic" {
		t.Fatalf("Render = %q, want %q", got, "Ceiling Mic")
	}
}

// A rule producing nothing is not an error: it is a rule with nothing to say
// about this row, and the caller writes no label rather than a blank one.
func TestRenderProducingOnlyWhitespaceIsEmpty(t *testing.T) {
	r := mustParse(t, "{{.Missing}}   {{.AlsoMissing}}")
	got, err := r.Render(label.Data{"TypeName": "Ceiling Mic"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != "" {
		t.Fatalf("Render = %q, want empty", got)
	}
}

// --- the sandbox --------------------------------------------------------

// The sandbox IS the data map. There is no filtering of a syntax, no denylist
// of field names: a secret is not reachable because it was never put in the
// map, and the map's value type is string, so there is nothing in it that has
// a field, a method, or a pointer to anything else.
//
// These three are the traversals a general template engine would otherwise
// offer over a richer environment: a field of a value, a method on it, and a
// function value invoked through the builtin `call`. Over a map of strings each
// one fails at execution, which is the caller's cue to degrade.
func TestARuleCannotTraverseOutOfTheDataMap(t *testing.T) {
	d := label.Data{"TypeName": "Ceiling Mic"}
	for _, tc := range []struct {
		name, src string
	}{
		{"field of a string", "{{.TypeName.Anything}}"},
		{"method on a string", "{{.TypeName.ToUpper}}"},
		{"call a data value", "{{call .TypeName}}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := mustParse(t, tc.src)
			if _, err := r.Render(d); err == nil {
				t.Fatalf("Render(%q) succeeded, want an execution error the caller degrades on", tc.src)
			} else if !errors.Is(err, label.ErrRuleFailed) {
				t.Fatalf("Render(%q) = %v, want ErrRuleFailed", tc.src, err)
			}
		})
	}
}

// index is the builtin that DOES work here, and it is worth pinning rather than
// assuming: text/template indexes maps and strings, so `index` over Data is a
// key lookup and over a value is a byte of a string already in the map. Neither
// reaches anything the map does not already carry, which is the property that
// matters. A key the map does not hold indexes to nothing, exactly as the
// field form does.
func TestIndexIsConfinedToTheDataMap(t *testing.T) {
	r := mustParse(t, `[{{index . "TypeName"}}][{{index . "Secret"}}]`)
	got, err := r.Render(label.Data{"TypeName": "Ceiling Mic"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != "[Ceiling Mic][]" {
		t.Fatalf("Render = %q, want %q", got, "[Ceiling Mic][]")
	}
}

// The other half of the same argument: a name that looks like a credential is
// not special-cased, it is simply not a key. It renders as nothing, exactly
// like any other word the map does not carry, and nothing about the engine had
// to know the word "secret" for that to be true.
func TestARuleAskingForACredentialGetsNothing(t *testing.T) {
	r := mustParse(t, "[{{.Secret}}{{.Password}}{{.Token}}{{.APIKey}}{{.Credential}}]")
	got, err := r.Render(label.Data{"TypeName": "Ceiling Mic"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != "[]" {
		t.Fatalf("Render = %q, want %q", got, "[]")
	}
}

// A rule that renders an absurd string is a render failure, not a row with a
// megabyte in a text column. printf is a text/template builtin that cannot be
// removed, and its width verb is the one way a closed string map can still
// produce output far larger than its inputs.
func TestRenderRefusesAnAbsurdlyLongResult(t *testing.T) {
	r := mustParse(t, `{{printf "%100000s" .TypeName}}`)
	if _, err := r.Render(label.Data{"TypeName": "x"}); err == nil {
		t.Fatal("Render of a 100000-wide printf succeeded, want a refusal")
	} else if !errors.Is(err, label.ErrRuleFailed) {
		t.Fatalf("Render = %v, want ErrRuleFailed", err)
	}
}

// --- the FuncMap --------------------------------------------------------

func TestUpperAndLower(t *testing.T) {
	r := mustParse(t, "{{.A | upper}}/{{.A | lower}}")
	got, err := r.Render(label.Data{"A": "Ceiling Mic"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != "CEILING MIC/ceiling mic" {
		t.Fatalf("Render = %q, want %q", got, "CEILING MIC/ceiling mic")
	}
}

// title upper-cases the first letter of each word and LEAVES THE REST ALONE.
// Lower-casing the remainder would be the obvious implementation and is wrong
// here: the strings this runs over are display names out of the catalog, so
// "Shure MXA920" would come back "Shure Mxa920", turning a correct product
// name into a misspelling of one.
func TestTitleUpperCasesEachWordWithoutFlatteningTheRest(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"ceiling mic", "Ceiling Mic"},
		{"ceiling-mic", "Ceiling-Mic"},
		{"Shure MXA920", "Shure MXA920"},
		{"wireless mic (handheld)", "Wireless Mic (Handheld)"},
		{"", ""},
	} {
		r := mustParse(t, "{{.A | title}}")
		got, err := r.Render(label.Data{"A": tc.in})
		if err != nil {
			t.Fatalf("Render(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("title(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The acronym seam (#684 builds the list itself). With no dictionary "av" is
// just a word and titles as "Av"; with one, the whole word is replaced. The
// engine's job here is only to have the seam, so the slice that owns the
// dictionary has somewhere to put it.
func TestTitleConsultsTheAcronymList(t *testing.T) {
	bare := label.New(nil)
	r, err := bare.Parse("{{.A | title}}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got, err := r.Render(label.Data{"A": "av rack"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != "Av Rack" {
		t.Fatalf("title with no acronyms = %q, want %q", got, "Av Rack")
	}

	withList := label.New([]string{"AV", "DSP"})
	r, err = withList.Parse("{{.A | title}}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got, err = r.Render(label.Data{"A": "av rack dsp"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != "AV Rack DSP" {
		t.Fatalf("title with acronyms = %q, want %q", got, "AV Rack DSP")
	}
}

// slug's output has to satisfy the entity-name rule (one kebab token,
// lowercase letters, digits and hyphens, no leading hyphen), because #687's
// name rules run through this same engine and whatever they produce is a name.
func TestSlugProducesAKebabToken(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Ceiling Mic", "ceiling-mic"},
		{"Shure MXA920", "shure-mxa920"},
		{"  Room 301  ", "room-301"},
		{"a//b__c", "a-b-c"},
		{"---", ""},
		{"", ""},
		{"-leading and trailing-", "leading-and-trailing"},
	} {
		r := mustParse(t, "{{.A | slug}}")
		got, err := r.Render(label.Data{"A": tc.in})
		if err != nil {
			t.Fatalf("Render(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("slug(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if got != "" && !kebab(got) {
			t.Fatalf("slug(%q) = %q, which is not a kebab token", tc.in, got)
		}
	}
}

func mustParse(t *testing.T, src string) *label.Rule {
	t.Helper()
	r, err := label.Basic.Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	return r
}

// kebab is validateEntityName's character rule, restated here rather than
// imported: internal/label deliberately does not depend on internal/storage
// (the engine is pure and the storage package is the one that owns the name
// rule), so the property slug promises is pinned locally.
func kebab(s string) bool {
	if s == "" || s[0] == '-' {
		return false
	}
	return strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-')
	}) < 0
}
