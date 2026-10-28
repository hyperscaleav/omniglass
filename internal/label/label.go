// Package label is the rule engine behind a generated label (#682): an
// operator-authored template, compiled once and executed over a closed map of
// facts about one entity.
//
// The template engine itself, its closed grammar and function set and the
// argument for both, is internal/sandbox (lifted out by #618 so a notification
// template runs through the same sandbox rather than a second one). What stays
// here is what makes a template a LABEL: the length ceiling, the whitespace
// normalization, and the two failure shapes a label's callers act on.
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
	"strings"

	"github.com/hyperscaleav/omniglass/internal/sandbox"
)

// MaxLen bounds a rendered label. A text column takes whatever it is given, and
// with the grammar closed the way left to reach this ceiling is literal text an
// operator typed, which is exactly the case it is for. An over-long render is a
// render FAILURE rather than a truncation: a label silently cut in half is a
// worse answer than no label, since the read ladder's next rung (the entity's
// name) is at least correct.
//
// It is a ceiling on OUTPUT and deliberately not relied on as a ceiling on
// WORK; the sandbox's grammar bounds that instead.
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

// Data is the closed, flat set of facts one rule executes over; see
// sandbox.Data for why it is a map of strings and nothing else.
type Data = sandbox.Data

// Engine compiles label rules against one acronym dictionary.
type Engine struct {
	e *sandbox.Engine
}

// Basic is the engine with no acronym dictionary, so `title` upper-cases the
// first letter of a word and knows no special cases.
var Basic = New(nil)

// New returns an engine whose `title` knows the given acronyms. Each entry is
// the form to emit ("AV", "DSP"); matching is case-insensitive on the whole
// word, so "av" and "Av" both title as "AV".
func New(acronyms []string) *Engine { return &Engine{e: sandbox.New(acronyms)} }

// FuncNames returns the closed function set's names, in documented order.
func FuncNames() []string { return sandbox.FuncNames() }

// Rule is a compiled label rule.
type Rule struct {
	t *sandbox.Template
}

// Parse compiles src. An error is [ErrRuleUnparseable] and belongs at the edit
// surface that accepted the text, not at the write path that would have used
// it. An empty rule parses and renders empty, which is how "this tier has no
// opinion" is written down when a caller needs a compiled rule anyway.
func (e *Engine) Parse(src string) (*Rule, error) {
	t, err := e.e.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrRuleUnparseable, strings.TrimPrefix(err.Error(), sandbox.ErrUnparseable.Error()+": "))
	}
	return &Rule{t: t}, nil
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
	out, err := r.t.Execute(d, MaxLen)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrRuleFailed, strings.TrimPrefix(err.Error(), sandbox.ErrFailed.Error()+": "))
	}
	return strings.Join(strings.Fields(out), " "), nil
}
