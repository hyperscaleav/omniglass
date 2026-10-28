package rules

import (
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/expression"
)

// The action site (#618): an action rule's `when`, a predicate over one
// alarm transition's facts. Its bindings are the alarm fact map a
// notification template reads (declared once in internal/storage, which a
// test holds this struct to), in snake_case, plus the severity's rank so a
// floor reads as a comparison (`severity_rank >= 40`, high and above).

// ActionInput is the action site's prepared input: one alarm transition.
type ActionInput struct {
	Component      string `expr:"component" doc:"The component's name"`
	ComponentLabel string `expr:"component_label" doc:"How the component reads: its label, else its name"`
	ComponentType  string `expr:"component_type" doc:"The label of the component type its product classifies it as"`
	Place          string `expr:"place" doc:"How the location the component sits at reads; empty when it sits at none of its own"`
	Severity       string `expr:"severity" doc:"The severity level the alarm was raised at, by name"`
	SeverityLabel  string `expr:"severity_label" doc:"That level's label, else its name"`
	SeverityRank   int    `expr:"severity_rank" doc:"That level's rank: higher is worse (the shipped high is 40, so severity_rank >= 40 is high and above)"`
	Message        string `expr:"message" doc:"The alarm's message"`
	Transition     string `expr:"transition" doc:"The edge: raised or cleared"`
	RaisedAt       string `expr:"raised_at" doc:"When the alarm was raised, RFC 3339 in UTC"`
	ClearedAt      string `expr:"cleared_at" doc:"When it was cleared, RFC 3339 in UTC; empty while raised"`
	Rule           string `expr:"rule" doc:"The event rule that raised it, by name; empty for an alarm an operator raised"`
	Link           string `expr:"link" doc:"The component's page in the console; empty while notification.console_url is unset"`
}

// actionEnv is the action site's environment: the criteria builtins and
// budget, a bool result.
var actionEnv = expression.MustEnv[ActionInput](expression.Bool,
	expression.WithBuiltins(expression.CriteriaBuiltins()...), expression.WithSite("action"))

// ActionEnv is the environment an action rule's when compiles against.
func ActionEnv() expression.Env { return actionEnv }

// When is a compiled action rule predicate; the empty source always holds.
type When struct {
	prog *expression.Program
}

// CompileWhen compiles an action rule's when at the action site: a refusal
// is an *expression.Error with its line and column.
func CompileWhen(src string) (*When, error) {
	if src == "" {
		return &When{}, nil
	}
	p, err := expression.Compile(src, actionEnv)
	if err != nil {
		return nil, err
	}
	return &When{prog: p}, nil
}

// Holds runs the predicate over one transition: the empty predicate always
// holds; a run that fails (over its budget) is an error the caller records.
func (w *When) Holds(in ActionInput) (bool, error) {
	if w == nil || w.prog == nil {
		return true, nil
	}
	out, err := w.prog.Run(in)
	if err != nil {
		return false, err
	}
	b, ok := out.(bool)
	if !ok {
		return false, fmt.Errorf("rules: action predicate yielded %T, not a bool", out)
	}
	return b, nil
}
