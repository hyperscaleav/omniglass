// Package label is the rule engine behind a generated label (#682): an
// operator-authored template, compiled once and executed over a closed map of
// facts about one entity.
//
// # Why a general engine rather than a small interpolation DSL
//
// A custom "${type} ${ordinal}" syntax was designed and rejected. The security
// property a label rule needs is not "a syntax that cannot express dangerous
// things", it is "an environment that contains nothing dangerous", and the
// environment is ours entirely: [Data] is a flat map of strings we build, key
// by key, from columns we chose. Given that, a hand-rolled parser is strictly
// more to write, document, test and maintain than Go's own, for a smaller
// feature set and a worse error message.
//
// # The sandbox is the data map
//
// This is the whole of the security argument, so it is worth stating in the
// negative: nothing in this package filters, escapes, or denies anything.
//
//   - [Data] is map[string]string. Its values have no fields, no methods worth
//     reaching, and no pointers to another entity, so field traversal, `call`
//     and `index` all fail at execution rather than reaching further.
//   - A key that is not in the map renders as the empty string (missingkey=zero,
//     set explicitly rather than relied on). There is no denylist of field names
//     because there is no mechanism by which a name outside the map resolves to
//     anything.
//   - A secret, a credential, a token: absent, structurally. Not filtered out of
//     a syntax, never put in.
//
// The corollary is a rule the caller must keep: whoever builds a [Data] owns the
// sandbox. Adding a key is adding to the attack surface; adding a key that is
// itself a container or carries a method would end the argument entirely.
//
// # Failure has two shapes and they are not the same
//
// A rule that does not PARSE is refused at rule-edit time and never stored
// ([ErrRuleUnparseable] -> 422). A rule that parses but fails to EXECUTE is
// found later, on a write path, with an entity half-built; the caller degrades
// to the next rung of the ladder ([ErrRuleFailed]) and must never propagate it.
package label

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"text/template"
	"unicode"
)

// MaxLen bounds a rendered label. text/template's printf is a builtin that
// cannot be removed, and its width verb is the one way a closed map of short
// strings still produces output far larger than its inputs; a text column will
// take whatever it is given, so the ceiling lives here. An over-long render is
// a render FAILURE rather than a truncation: a label silently cut in half is a
// worse answer than no label, since the read ladder's next rung (the entity's
// name) is at least correct.
const MaxLen = 200

var (
	// ErrRuleUnparseable is a rule that does not compile. It is the edit-time
	// refusal (422), and the reason a rule is parsed at the moment it is
	// written rather than only at the moment it is used.
	ErrRuleUnparseable = errors.New("label: rule does not parse")

	// ErrRuleFailed is a compiled rule that failed while executing over one
	// entity's facts. Reaching for something the data map does not carry is
	// the usual cause. The caller degrades; it never propagates.
	ErrRuleFailed = errors.New("label: rule failed to render")
)

// Data is the closed, flat set of facts one rule executes over: exactly the
// scalars its caller chose to expose, and nothing reachable from them.
//
// The value type is string rather than any, deliberately, and it does three
// jobs at once. An absent fact renders as nothing instead of "<nil>" or
// "<no value>". An absent fact is falsy, so {{if .Ordinal}} is the natural way
// to write a rule that adapts. And the map cannot be made to carry a struct, a
// pointer or a func by a later caller in a hurry, so the sandbox stays closed
// by the type rather than by everyone's good intentions.
type Data map[string]string

// Engine holds the compiled FuncMap a rule is parsed against. One is built per
// acronym dictionary, not per rule: parsing binds the function set, so a rule
// compiled by an engine titles words the way that engine's dictionary says.
type Engine struct {
	funcs template.FuncMap
}

// Basic is the engine with no acronym dictionary, so `title` upper-cases the
// first letter of a word and knows no special cases. It is what every caller
// uses today; #684 (the acronym list) is the slice that builds a real
// dictionary and hands it to [New].
var Basic = New(nil)

// New returns an engine whose `title` knows the given acronyms. Each entry is
// the form to emit ("AV", "DSP"); matching is case-insensitive on the whole
// word, so "av" and "Av" both title as "AV".
func New(acronyms []string) *Engine {
	dict := make(map[string]string, len(acronyms))
	for _, a := range acronyms {
		dict[strings.ToLower(a)] = a
	}
	return &Engine{funcs: template.FuncMap{
		"title": func(s string) string { return titleWords(s, dict) },
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
		"slug":  slug,
	}}
}

// FuncNames returns the closed FuncMap's names, in the order they are
// documented. Exported so the set can be pinned by a test rather than only
// described by a comment: adding a function is then a deliberate act with a
// test to change.
func FuncNames() []string { return []string{"title", "upper", "lower", "slug"} }

// Rule is a compiled label rule.
type Rule struct {
	tpl *template.Template
}

// Parse compiles src. An error is [ErrRuleUnparseable] and belongs at the edit
// surface that accepted the text, not at the write path that would have used
// it. An empty rule parses and renders empty, which is how "this tier has no
// opinion" is written down when a caller needs a compiled rule anyway.
func (e *Engine) Parse(src string) (*Rule, error) {
	tpl, err := template.New("label").
		Funcs(e.funcs).
		Option("missingkey=zero").
		Parse(src)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrRuleUnparseable, err)
	}
	return &Rule{tpl: tpl}, nil
}

// Render executes r over d and normalizes the result: runs of whitespace
// collapse to one space and the ends are trimmed.
//
// The normalization is not cosmetic, it is what keeps the simple form of a rule
// usable. "{{.TypeName}} {{.Ordinal}}" over a row with no ordinal would
// otherwise store "Ceiling Mic " with a trailing space, so every rule would
// have to guard every separator with {{if}} to be correct. Collapsing means the
// guard is only needed when the separator is not a space.
//
// An empty result is not an error. It is a rule with nothing to say about this
// row, and the caller stores no label rather than a blank one.
func (r *Rule) Render(d Data) (string, error) {
	var b strings.Builder
	w := &capped{w: &b, left: MaxLen}
	if err := r.tpl.Execute(w, d); err != nil {
		return "", fmt.Errorf("%w: %s", ErrRuleFailed, err)
	}
	return strings.Join(strings.Fields(b.String()), " "), nil
}

// errTooLong is capped's refusal, wrapped by Execute into the error Render maps
// to ErrRuleFailed.
var errTooLong = fmt.Errorf("rendered label exceeds %d characters", MaxLen)

// capped is an io.Writer that refuses past a byte budget, so an over-long
// render aborts the template mid-execution instead of building the whole string
// and being measured afterwards.
type capped struct {
	w    io.Writer
	left int
}

func (c *capped) Write(p []byte) (int, error) {
	if len(p) > c.left {
		return 0, errTooLong
	}
	c.left -= len(p)
	return c.w.Write(p)
}

// wordRe matches one word for titleWords and is deliberately letters and digits
// only: everything between words (spaces, hyphens, parentheses, slashes) is
// separator and is preserved untouched.
var wordRe = regexp.MustCompile(`[\p{L}\p{N}]+`)

// titleWords upper-cases each word's first letter and LEAVES THE REST ALONE.
//
// Lower-casing the remainder is what strings.Title did and what most
// implementations do, and it is wrong for this input. The strings a label rule
// titles are display names out of the catalog, so flattening the tail turns
// "Shure MXA920" into "Shure Mxa920": a correct product name rendered as a
// misspelling of one. Casing that is already deliberate is left deliberate, and
// the acronym dictionary is how a word gets a case the operator did not type.
func titleWords(s string, acronyms map[string]string) string {
	return wordRe.ReplaceAllStringFunc(s, func(w string) string {
		if a, ok := acronyms[strings.ToLower(w)]; ok {
			return a
		}
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		return string(r)
	})
}

// slug reduces a string to one kebab token: lowercase letters, digits and
// single hyphens, with no hyphen at either end.
//
// The output rule is validateEntityName's, not a stylistic choice. #687's name
// rules run through this same engine, and whatever a name rule produces is a
// name, so the one function an operator will reach for to make a fact
// name-shaped has to actually produce a legal one.
func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		default:
			dash = true
		}
	}
	return b.String()
}
