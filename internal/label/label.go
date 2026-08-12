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
// # The sandbox is the data map AND the grammar
//
// There are two halves, and they are the same argument twice: an allowlist of
// what the rule can reach, rather than a denylist of what it must not do.
//
// The data half. Nothing here filters, escapes, or denies a VALUE.
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
// The grammar half ([checkTree]). A closed data map bounds what a rule can
// READ; it does not bound what a rule can DO, and the two are not the same
// problem. [MaxLen] caps the bytes reaching the writer, but a value built
// inside a pipeline is materialized by fmt and never written, so the cap never
// sees it: 437 bytes of rule (`{{$a := printf "%1000s" .TypeName}}` and
// fourteen `{{$a = printf "%s%s" $a $a}}`) allocates 85 MB and writes 8, with
// every further doubling 35 more bytes of rule for twice the memory. Twenty of
// them OOMs the single binary, and the rule is stored, so it re-triggers on
// every later write to any entity of that type.
//
// Refusing `:=` would not fix it, because the nested form
// `{{(printf "%s%s" (printf ...) ...) | slug}}` does the same with no
// assignment; and printf is a text/template BUILTIN, so the FuncMap never
// granted it and removing a FuncMap entry never takes it away. So the grammar
// is an allowlist too: a closed set of node types and a closed set of function
// names, checked against the parsed tree, with everything text/template
// otherwise offers refused at PARSE time. That refusal is rule-EDIT time, so
// such a rule is never stored, and a rule that predates the check still cannot
// execute what it names, because the check lives in [Engine.Parse] and every
// render parses.
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
	"text/template/parse"
	"unicode"
)

// MaxLen bounds a rendered label. A text column takes whatever it is given, and
// with the grammar closed the way left to reach this ceiling is literal text an
// operator typed, which is exactly the case it is for. An over-long render is a
// render FAILURE rather than a truncation: a label silently cut in half is a
// worse answer than no label, since the read ladder's next rung (the entity's
// name) is at least correct.
//
// It is a ceiling on OUTPUT and deliberately not relied on as a ceiling on
// WORK. Bytes that never reach the writer are never counted, which is what
// [checkTree] exists to bound instead.
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
		"words": words,
	}}
}

// FuncNames returns the closed FuncMap's names, in the order they are
// documented. Exported so the set can be pinned by a test rather than only
// described by a comment: adding a function is then a deliberate act with a
// test to change.
//
// It is also what holds the three places a function has to appear to exist
// honest: a test walks this set and parses each name in a real rule, so a name
// published here that is missing from the FuncMap or from [allowedFuncs] fails
// rather than being quietly unreachable.
func FuncNames() []string { return []string{"title", "upper", "lower", "slug", "words"} }

// Rule is a compiled label rule.
type Rule struct {
	tpl *template.Template
}

// Parse compiles src. An error is [ErrRuleUnparseable] and belongs at the edit
// surface that accepted the text, not at the write path that would have used
// it. An empty rule parses and renders empty, which is how "this tier has no
// opinion" is written down when a caller needs a compiled rule anyway.
func (e *Engine) Parse(src string) (*Rule, error) {
	tpl, err := template.New(rootName).
		Funcs(e.funcs).
		Option("missingkey=zero").
		Parse(src)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrRuleUnparseable, err)
	}
	if err := checkTree(tpl); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrRuleUnparseable, err)
	}
	return &Rule{tpl: tpl}, nil
}

// rootName is the one template a rule may consist of. A second one can only
// come from a {{define}}, which checkTree refuses.
const rootName = "label"

// allowedFuncs is the closed set of function names a rule may name: the
// FuncMap's own ([FuncNames]), plus the comparison and logic builtins.
//
// The builtins are here because the parser accepts them whether or not the
// FuncMap does, so a set that listed only our own functions would be describing
// a smaller language than the one being run. Each admitted one returns a bool
// from values it does not copy, so none can be made to do work proportional to
// anything but the rule's own length.
//
// Everything else is refused BY NAME rather than by what it appears to do:
// printf, print and println build strings, index and slice and call reach into
// values, html and js and urlquery escape for an output this is not. len is
// refused for having no use a label rule's {{if .X}} does not already cover; it
// is harmless, and admitting a harmless function is still widening the language
// for nothing.
var allowedFuncs = map[string]bool{
	"title": true, "upper": true, "lower": true, "slug": true, "words": true,
	"and": true, "or": true, "not": true,
	"eq": true, "ne": true, "lt": true, "le": true, "gt": true, "ge": true,
}

// checkTree walks a parsed rule and refuses any node type or function name
// outside the closed set. See the package comment for why an allowlist rather
// than a cap.
//
// {{if}} is the one branching form admitted, because the shipped rules need it
// to suppress an absent ordinal and because a branch cannot repeat. {{range}}
// can, and {{with}} only restates {{if}} while rebinding dot, so neither earns
// the surface. {{template}} would invoke another tree and {{define}} would
// leave one, so both go, and the associated-template count is checked directly
// rather than inferred from the absence of a node: a {{define}} is stripped
// from the root tree, so it is invisible to the walk.
func checkTree(tpl *template.Template) error {
	if len(tpl.Templates()) > 1 {
		return errors.New("a rule may not define another template")
	}
	if tpl.Tree == nil || tpl.Tree.Root == nil {
		return nil
	}
	return checkNode(tpl.Tree.Root)
}

func checkNode(n parse.Node) error {
	switch n := n.(type) {
	case nil:
		return nil
	case *parse.TextNode, *parse.StringNode, *parse.NumberNode, *parse.BoolNode,
		*parse.DotNode, *parse.FieldNode, *parse.CommentNode:
		return nil
	case *parse.IdentifierNode:
		if !allowedFuncs[n.Ident] {
			return fmt.Errorf("function %q is not available to a label rule", n.Ident)
		}
		return nil
	case *parse.ListNode:
		for _, c := range n.Nodes {
			if err := checkNode(c); err != nil {
				return err
			}
		}
		return nil
	case *parse.ActionNode:
		return checkNode(n.Pipe)
	case *parse.PipeNode:
		if len(n.Decl) > 0 {
			return errors.New("a rule may not declare a variable")
		}
		for _, c := range n.Cmds {
			if err := checkNode(c); err != nil {
				return err
			}
		}
		return nil
	case *parse.CommandNode:
		for _, a := range n.Args {
			if err := checkNode(a); err != nil {
				return err
			}
		}
		return nil
	case *parse.IfNode:
		if err := checkNode(n.Pipe); err != nil {
			return err
		}
		if err := checkNode(n.List); err != nil {
			return err
		}
		if n.ElseList == nil {
			return nil
		}
		return checkNode(n.ElseList)
	default:
		return fmt.Errorf("%s is not available to a label rule", describe(n))
	}
}

// describe names a refused construct the way an operator wrote it, since
// "*parse.RangeNode is not available" is a Go type talking to somebody who
// typed a template.
func describe(n parse.Node) string {
	switch n.(type) {
	case *parse.RangeNode:
		return "{{range}}"
	case *parse.WithNode:
		return "{{with}}"
	case *parse.TemplateNode:
		return "{{template}}"
	case *parse.VariableNode:
		return "a variable"
	case *parse.ChainNode:
		return "a field of a parenthesized expression"
	case *parse.BreakNode:
		return "{{break}}"
	case *parse.ContinueNode:
		return "{{continue}}"
	case *parse.NilNode:
		return "nil"
	}
	return fmt.Sprintf("%T", n)
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
// separator and is preserved untouched. Preserved, not replaced: turning a
// name's hyphens into spaces is [words], and keeping the two apart is what lets
// a rule title a display name without rewriting the punctuation in it.
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

// sepRe matches a run of the separators a NAME is built from. Exactly two, and
// not the general "anything that is not a letter or a digit" [wordRe] uses:
// words runs over facts that are already prose as well as over names, and a
// display name's parentheses, slashes and periods are punctuation somebody
// chose rather than separators to spend.
var sepRe = regexp.MustCompile(`[-_]+`)

// words turns a kebab or snake token into the words in it, which is what lets a
// rule read a NAME as something a person would say: `{{title (words .Name)}}`
// renders `north-wing` as "North Wing" and, through the acronym dictionary,
// `hq-west` as "HQ West".
//
// It is slug's opposite number, and the reason the dictionary reaches anything
// at all: `title` upper-cases each word and leaves the separator standing
// (`{{title .Name}}` gives "North-Wing"), so before this there was no rule an
// operator could write that turned a name into a friendly label.
//
// The edges, all three decided rather than fallen into:
//
//   - A RUN of separators is one space, so `north--wing` is not "north  wing".
//   - A leading or trailing run goes entirely rather than becoming an edge
//     space. A name cannot begin with one, but a rule may be handed any fact,
//     and a stored label with a space in front is a defect somebody has to
//     notice before they can fix it.
//   - Everything else is untouched, including whitespace the fact already
//     carried. words rewrites separators; it is not a normalizer.
//
// [Rule.Render] collapses whitespace and trims on the way out, so the first two
// are invisible in a rendered label and visible to a rule that COMPARES the
// value (`{{if eq (words .Name) "north wing"}}`). They are pinned there rather
// than left to the writer's normalization, because a function whose contract is
// only true because of what happens downstream of it is one refactor from being
// false.
func words(s string) string {
	return sepRe.ReplaceAllString(strings.Trim(s, "-_"), " ")
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
