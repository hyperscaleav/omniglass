package label_test

import (
	"errors"
	"strconv"
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

// The FuncMap is CLOSED: exactly title, upper, lower, slug, words. A function
// name outside that set fails at parse time (above), and this pins the set
// itself so growing it is a deliberate act with a test to change, not a
// drive-by.
func TestFuncMapIsExactlyTheClosedSet(t *testing.T) {
	want := []string{"title", "upper", "lower", "slug", "words"}
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

// A function lives in THREE places or it does not exist: the FuncMap (or the
// parser refuses the name as undefined), the AST allowlist (or checkTree refuses
// it), and FuncNames (the pinned set above). Two of the three is the shape of
// the mistake this catches, and it is silent in either direction: a name in the
// FuncMap alone parses nowhere, and a name in the allowlist alone is only a hole
// in the allowlist.
//
// So the set is walked rather than described. Every name FuncNames publishes is
// parsed in a real rule here, which is the assertion that the other two places
// agree with it; the closed-set test above is what stops a name being added to
// the FuncMap and the allowlist while FuncNames stays quiet.
func TestEveryPublishedFuncNameIsReachableFromARule(t *testing.T) {
	for _, name := range label.FuncNames() {
		src := "{{.A | " + name + "}}"
		if _, err := label.Basic.Parse(src); err != nil {
			t.Errorf("Parse(%q) = %v; %q is published by FuncNames but not reachable: it is missing from the FuncMap or from the AST allowlist", src, err, name)
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

// `call` and `index` are the two builtins that would reach a value some other
// way, and both are now refused by name at parse time rather than left to fail
// (or, in index's case, quietly succeed) at execution. They are listed with the
// rest of the closed grammar in
// TestParseRefusesTheConstructsThatMakeWorkUnbounded; the field form above is
// what remains, and it still degrades.

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
// megabyte in a text column. With the grammar closed (below) the only way left
// to reach the ceiling is literal text an operator typed, which is exactly the
// case the ceiling is for.
func TestRenderRefusesAnAbsurdlyLongResult(t *testing.T) {
	r := mustParse(t, strings.Repeat("x", label.MaxLen+1))
	if _, err := r.Render(label.Data{"TypeName": "x"}); err == nil {
		t.Fatal("Render of an over-long literal succeeded, want a refusal")
	} else if !errors.Is(err, label.ErrRuleFailed) {
		t.Fatalf("Render = %v, want ErrRuleFailed", err)
	}
}

// --- the closed grammar -------------------------------------------------

// THE OUTPUT CEILING IS NOT A WORK CEILING, and that is why the grammar is an
// allowlist rather than a bigger cap.
//
// A value built inside a pipeline is materialized by fmt and never written, so
// the capped writer never sees it. `{{$a := printf "%1000s" .TypeName}}` plus
// fourteen `{{$a = printf "%s%s" $a $a}}` plus `{{len $a}}` is 437 bytes of
// operator-authored rule that allocates 85 MB and writes 8, and each further
// doubling is 35 more bytes of rule for twice the memory: twenty of them is
// several GB and an unrecoverable OOM of the single binary, re-triggered on
// every later write to any entity of that type.
//
// Banning `:=` does not fix it. The nested form
// `{{len (printf "%s%s" (printf "%s%s" ...) ...)}}` does the same with no
// assignment at all. And printf is a text/template BUILTIN, so it is not
// something the FuncMap granted and not something removing the FuncMap entry
// takes away.
//
// So the fix is the same argument that justified the closed data map, applied
// to the other half of the sandbox: the grammar is a closed set of node types
// and function names, and everything text/template otherwise offers is refused
// at PARSE time, which is rule-edit time, before it can be stored.
func TestParseRefusesTheConstructsThatMakeWorkUnbounded(t *testing.T) {
	assign := `{{$a := printf "%1000s" .TypeName}}`
	for range 14 {
		assign += `{{$a = printf "%s%s" $a $a}}`
	}
	assign += `{{len $a}}`

	// Deliberately piped into an ALLOWED function at the top, so the refusal
	// has to come from the nested printf rather than from the outer call. This
	// is the form that defeats the obvious fix of banning assignment.
	nested := `(printf "%1000s" .TypeName)`
	for range 8 {
		nested = `(printf "%s%s" ` + nested + ` ` + nested + `)`
	}
	nested = `{{` + nested + ` | slug}}`

	// The same doubling in ARGUMENT position behind an allowed function, so
	// the forbidden identifier is not the first thing the walk sees. A checker
	// that inspected only a command's head, or only the outermost call, would
	// pass this one, and it is the compact form: no assignment, no growth in
	// the source.
	inArg := `(printf "%1000s" .TypeName)`
	for range 8 {
		inArg = `(printf "%s%s" ` + inArg + ` ` + inArg + `)`
	}
	inArg = `{{slug ` + inArg + `}}`

	for _, tc := range []struct{ name, src string }{
		{"the assignment form", assign},
		{"the nested-pipeline form, which needs no assignment", nested},
		{"a forbidden function in a non-first argument", inArg},
		{"a forbidden function one level down in an argument", `{{slug (printf "%1000s" .TypeName)}}`},
		{"a bare declaration nothing refers to", `{{$a := .TypeName}}ok`},
		{"printf on its own", `{{printf "%100000s" .TypeName}}`},
		{"print", `{{print .TypeName .TypeName}}`},
		{"println", `{{println .TypeName}}`},
		{"a variable declaration", `{{$a := .TypeName}}{{$a}}`},
		{"range, the other way to repeat work", `{{range .}}{{.}}{{end}}`},
		{"with, which rebinds dot", `{{with .TypeName}}{{.}}{{end}}`},
		{"template invocation", `{{template "other"}}`},
		{"define, which would leave an associated template", `{{define "other"}}x{{end}}{{.TypeName}}`},
		{"call", `{{call .TypeName}}`},
		{"index", `{{index . "TypeName"}}`},
		{"slice", `{{slice .TypeName 0 1}}`},
		{"len", `{{len .TypeName}}`},
		{"html", `{{html .TypeName}}`},
		{"js", `{{js .TypeName}}`},
		{"urlquery", `{{urlquery .TypeName}}`},
		{"a parenthesized chain", `{{(.TypeName).Anything}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := label.Basic.Parse(tc.src); err == nil {
				t.Fatalf("Parse accepted %.60q, want a refusal at rule-edit time", tc.src)
			} else if !errors.Is(err, label.ErrRuleUnparseable) {
				t.Fatalf("Parse = %v, want ErrRuleUnparseable", err)
			}
		})
	}
}

// The other half of an allowlist: what it still admits. Comparison and logic
// are the builtins a real rule needs, and every one of them returns a bool from
// values it does not copy, so none can be made to do work proportional to
// anything but the rule's own length.
func TestParseAdmitsTheComparisonAndLogicBuiltins(t *testing.T) {
	for _, src := range []string{
		`{{if eq .TypeName "Display"}}D{{end}}`,
		`{{if ne .TypeName "Display"}}not D{{end}}`,
		`{{if and .TypeName .Ordinal}}both{{end}}`,
		`{{if or .TypeName .Ordinal}}either{{end}}`,
		`{{if not .Ordinal}}none{{end}}`,
		`{{if lt .Ordinal "5"}}low{{else}}high{{end}}`,
		`{{if le .Ordinal "5"}}x{{end}}{{if gt .Ordinal "5"}}y{{end}}{{if ge .Ordinal "5"}}z{{end}}`,
		`{{/* a comment */}}{{.TypeName}}`,
	} {
		if _, err := label.Basic.Parse(src); err != nil {
			t.Fatalf("Parse(%q) = %v, want no error", src, err)
		}
	}
}

// The grammar is closed at the ENGINE, not at one entry point, so a rule that
// slipped in before this landed still cannot execute what it names. Belt and
// braces on the one thing that would otherwise be a stored-data problem rather
// than a code problem.
func TestAnAlreadyStoredForbiddenRuleStillCannotRender(t *testing.T) {
	if _, err := label.Basic.Parse(`{{printf "%1000s" .TypeName}}`); err == nil {
		t.Fatal("precondition: printf parsed")
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

// words is slug's opposite number and the one this engine was missing: it turns
// the separators a NAME is built from back into spaces, so a rule can read a
// kebab name as the words an operator meant.
//
// The assertions run through `eq` rather than through the rendered string,
// deliberately. Render collapses runs of whitespace and trims the ends, so a
// bare {{words .A}} could not tell a function that collapses from one that does
// not; comparing the intermediate value is the only way the contract is
// asserted rather than the writer's normalization.
func TestWordsTurnsSeparatorsIntoSpaces(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"north-wing", "north wing"},
		{"hq-west", "hq west"},
		{"boardroom", "boardroom"},
		{"loading_dock", "loading dock"},
		{"level-1-north_east", "level 1 north east"},
		// A run of separators is ONE space, so a doubled hyphen does not open a
		// gap in the middle of a label.
		{"north--wing", "north wing"},
		{"north-_-wing", "north wing"},
		// A leading or trailing run goes entirely rather than becoming an edge
		// space: a name may not start with one, but a rule can be handed any
		// fact, and " North Wing" with a space in front is a stored label
		// somebody has to notice and fix.
		{"-north-wing-", "north wing"},
		{"___", ""},
		{"", ""},
		// Everything that is not one of the two separators is left exactly as it
		// was, including whitespace the fact already carried and the punctuation
		// a catalog display name is full of.
		{"Shure MXA920", "Shure MXA920"},
		{"Wireless Mic (Handheld)", "Wireless Mic (Handheld)"},
		{"1", "1"},
	} {
		r := mustParse(t, `{{if eq (words .A) `+strconv.Quote(tc.want)+`}}yes{{else}}no{{end}}`)
		got, err := r.Render(label.Data{"A": tc.in})
		if err != nil {
			t.Fatalf("Render(%q): %v", tc.in, err)
		}
		if got != "yes" {
			// Rendered separately so a failure reports what it actually
			// produced rather than only that it differed.
			shown := mustParse(t, `[{{words .A}}]`)
			actual, _ := shown.Render(label.Data{"A": tc.in})
			t.Errorf("words(%q) is not %q (rendered %s)", tc.in, tc.want, actual)
		}
	}
}

// The composition this whole function exists for, and the reason the acronym
// dictionary (#684) finally reaches something: title alone upper-cases each word
// and leaves the separator standing, so `{{title .Name}}` on a kebab name gives
// "North-Wing". words in front of it is what makes a friendly label out of a
// name, and the dictionary then runs over the words it produced.
func TestTitleOverWordsIsHowANameBecomesALabel(t *testing.T) {
	for _, tc := range []struct {
		name     string
		acronyms []string
		in, want string
	}{
		{"a two-word name", nil, "north-wing", "North Wing"},
		{"a word the dictionary owns", []string{"HQ"}, "hq-west", "HQ West"},
		{"and without it, ordinary title case", nil, "hq-west", "Hq West"},
		{"a one-word name is just titled", nil, "boardroom", "Boardroom"},
		{"a positional name has nothing to title", nil, "1", "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eng := label.New(tc.acronyms)
			r, err := eng.Parse("{{title (words .Name)}}")
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			got, err := r.Render(label.Data{"Name": tc.in})
			if err != nil {
				t.Fatalf("Render(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("title(words(%q)) = %q, want %q", tc.in, got, tc.want)
			}
		})
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
