package rules

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/expression"
)

// The expression sites (#958): each place an operator writes an expression
// is declared once here, its environment (bindings, builtins, budget) per
// value type, so the validate route, the editor's completion and the docs
// table all read one declaration (SitesFactsJSON, rendered by make gen)
// rather than a hand-written list. A later site registers here once.

// The value types a criteria site's value binding takes.
const (
	// ValueNumber is a metric's value: a float64.
	ValueNumber = "number"
	// ValueText is a string or json property's value: text.
	ValueText = "text"
	// ValueBool is a bool property's value.
	ValueBool = "bool"
)

// SiteVariant is one environment a site compiles against: which value type
// it is for and when that applies.
type SiteVariant struct {
	ValueType string
	When      string
	Env       expression.Env
}

// Site is one place expressions are written.
type Site struct {
	Name    string
	Summary string
	// KeyRequired is a site whose value type a sample key selects, and so
	// must be given one (a rule always reads a key).
	KeyRequired bool
	Variants    []SiteVariant
	// Bindings describes a site whose bindings are authored with each
	// expression (a calc rule's inputs), so its variants declare none.
	Bindings string
	// ReservedNames are the names an authored binding (a calc rule's alias)
	// may not take; nil for a site that authors none.
	ReservedNames []string
}

// Variant is the site's environment for a value type.
func (s Site) Variant(valueType string) (SiteVariant, bool) {
	for _, v := range s.Variants {
		if v.ValueType == valueType {
			return v, true
		}
	}
	return SiteVariant{}, false
}

// calcVariant is the calc site's environment for an output of one value
// type, before a rule's inputs are bound: the builtins and budget every calc
// expression gets (CalcEnv binds the inputs).
func calcVariant(valueType, when string) SiteVariant {
	result, _ := resultFor(valueType)
	return SiteVariant{ValueType: valueType, When: when, Env: expression.MustEnvFor(reflect.TypeFor[struct{}](), result, calcOptions()...)}
}

// sites is the registry, in name order.
var sites = []Site{{
	Name: "calc",
	Summary: "A calc rule's reduce: one value of the output key's type, from the rule's aliased inputs on one owner. " +
		"The output key types the result, so a key is required.",
	KeyRequired: true,
	Variants: []SiteVariant{
		calcVariant(ValueNumber, "a metric output"),
		calcVariant(ValueText, "a string or json property output"),
		calcVariant(ValueBool, "a bool property output"),
	},
	Bindings: "Each of the rule's inputs by its alias, as the list of its current values, typed by its key (a metric " +
		"is a number, a property text, a bool property a bool). For a component owner, one value per instance: the " +
		"named instances, the aggregate instance only when it is the key's one, an instance not heard from in an hour " +
		"left out. For a system owner, every role-bound member's values, ordered by member name then id; and members: " +
		"one record per member, holding id, name, value.<alias> (the member's own current value, the type's zero when " +
		"it has none) and has.<alias> (whether it holds one), so one member's inputs stay together. A list function " +
		"(avg, max, min, sum, majority, any, all) folds a list into one value.",
	ReservedNames: CalcReservedNames(),
}, {
	Name: "criteria",
	Summary: "An event rule's fire_criteria and clear_criteria: true or false for one arriving sample. " +
		"The sample key's lane and data type type the value, so a key is required.",
	KeyRequired: true,
	Variants: []SiteVariant{
		{ValueType: ValueNumber, When: "a metric", Env: metricEnv},
		{ValueType: ValueText, When: "a string or json property", Env: textEnv},
		{ValueType: ValueBool, When: "a bool property", Env: boolEnv},
	},
}}

// LookupSite is the registered site of that name.
func LookupSite(name string) (Site, bool) {
	for _, s := range sites {
		if s.Name == name {
			return s, true
		}
	}
	return Site{}, false
}

// SiteNames are the registered sites' names, sorted.
func SiteNames() []string {
	out := make([]string, 0, len(sites))
	for _, s := range sites {
		out = append(out, s.Name)
	}
	slices.Sort(out)
	return out
}

// ValueTypeFor is the value type a sample key's lane and data type
// select (CriteriaEnv's choice, named).
func ValueTypeFor(lane Lane, dataType string) string {
	switch {
	case lane == LaneMetric:
		return ValueNumber
	case dataType == "bool":
		return ValueBool
	}
	return ValueText
}

// SitesFacts is the generated description of every site the docs render
// (docs/src/generated/expressions.json).
type SitesFacts struct {
	Comment string      `json:"//"`
	Sites   []SiteFacts `json:"sites"`
}

// SiteFacts is one site as the docs and the validate route describe it.
type SiteFacts struct {
	Name        string         `json:"name"`
	Summary     string         `json:"summary"`
	KeyRequired bool           `json:"key_required"`
	Variants    []VariantFacts `json:"variants"`
	Bindings    string         `json:"bindings,omitempty"`
	Builtins    []string       `json:"builtins"`
	// Functions are the list functions the site admits, each with its cost
	// argument.
	Functions []FunctionFacts `json:"functions,omitempty"`
	// ReservedNames are the names an authored binding may not take.
	ReservedNames []string    `json:"reserved_names,omitempty"`
	Budget        BudgetFacts `json:"budget"`
}

// FunctionFacts is one list function as the docs describe it.
type FunctionFacts struct {
	Name string `json:"name"`
	Cost string `json:"cost"`
}

// VariantFacts is one value type's bindings.
type VariantFacts struct {
	ValueType string               `json:"value_type"`
	When      string               `json:"when"`
	Bindings  []expression.Binding `json:"bindings"`
}

// BudgetFacts is a site's run budget.
type BudgetFacts struct {
	Iterations int    `json:"iterations"`
	Memory     int    `json:"memory"`
	Deadline   string `json:"deadline"`
}

// Facts describes a site from its declaration: every variant's bindings
// read from its Env, and the builtins and budget the site's Env admits
// (the same for every variant of a site).
func (s Site) Facts() SiteFacts {
	f := SiteFacts{Name: s.Name, Summary: s.Summary, KeyRequired: s.KeyRequired, Bindings: s.Bindings, ReservedNames: s.ReservedNames}
	for _, v := range s.Variants {
		b := v.Env.Bindings()
		if b == nil {
			b = []expression.Binding{}
		}
		f.Variants = append(f.Variants, VariantFacts{ValueType: v.ValueType, When: v.When, Bindings: b})
	}
	if len(s.Variants) > 0 {
		env := s.Variants[0].Env
		f.Builtins = env.Builtins()
		for _, n := range env.ListFunctions() {
			f.Functions = append(f.Functions, FunctionFacts{Name: n, Cost: expression.ListFunctionCost(n)})
		}
		b := env.Budget()
		f.Budget = BudgetFacts{Iterations: b.Iterations, Memory: b.Memory, Deadline: b.Deadline.String()}
	}
	return f
}

// SitesFactsJSON renders every site for the docs, written by make gen
// (cmd/exprgen); TestSitesFactsMatchCommitted is its drift gate.
func SitesFactsJSON() ([]byte, error) {
	doc := SitesFacts{Comment: "Generated by cmd/exprgen from internal/rules/site.go (make gen). Do not edit."}
	for _, name := range SiteNames() {
		s, _ := LookupSite(name)
		doc.Sites = append(doc.Sites, s.Facts())
	}
	b, err := json.MarshalIndent(doc, "", " ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// CalcNamesTS renders the calc site's alias rules for the console
// (web/src/lib/calc_names.gen.ts, written by make gen), so the form and the
// editor refuse exactly the aliases the server refuses;
// TestCalcNamesMatchCommitted is its drift gate.
func CalcNamesTS() []byte {
	var b strings.Builder
	b.WriteString("// Generated by cmd/exprgen from internal/rules (make gen). Do not edit.\n")
	b.WriteString("// The calc site's alias rules (#1018): an identifier of at most CALC_ALIAS_MAX,\n")
	b.WriteString("// not one of CALC_RESERVED_NAMES (keywords, builtins, list functions, the site's own bindings).\n\n")
	fmt.Fprintf(&b, "export const CALC_ALIAS_MAX = %d;\n\n", expression.MaxBindingName)
	b.WriteString("export const CALC_RESERVED_NAMES: readonly string[] = [\n")
	for _, n := range CalcReservedNames() {
		fmt.Fprintf(&b, "  %q,\n", n)
	}
	b.WriteString("];\n\n")
	b.WriteString("export const CALC_LIST_FUNCTIONS: readonly string[] = [\n")
	for _, n := range expression.ListFunctions() {
		fmt.Fprintf(&b, "  %q,\n", n)
	}
	b.WriteString("];\n")
	return []byte(b.String())
}
