package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/expression"
	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

type validateExpressionInput struct {
	Body struct {
		Site      string          `json:"site" enum:"calc,criteria" doc:"Where the expression is written: calc, a calc rule's reduce; criteria, an event rule's fire_criteria or clear_criteria"`
		Source    string          `json:"source" doc:"The expression"`
		Key       string          `json:"key,omitempty" required:"false" doc:"The sample key that types the expression (required at both sites). For criteria, the key the rule reads: its lane and data type type the value (a metric is a number; a property is text, or a bool for a bool property). For calc, the rule's output key: its type is the result the reduce must yield"`
		OwnerKind string          `json:"owner_kind,omitempty" required:"false" enum:"component,system" doc:"For calc, what the rule derives on: each input binds by its alias as the list of its current values (one per instance on a component, every member's on a system); a system also binds members, one record per role-bound member with value.<alias> and has.<alias> per input; defaults to component"`
		Inputs    []calcInputBody `json:"inputs,omitempty" required:"false" doc:"For calc (required there), the rule's inputs: each bound by its alias, typed by its key (by name or uuid)"`
	}
}

type expressionDiagnostic struct {
	Line     int                `json:"line" doc:"The 1-based line of the diagnostic; 0 when it has none (a length cap, an input the expression never names)"`
	Column   int                `json:"column" doc:"The 1-based column of the diagnostic; 0 when it has none"`
	Phase    string             `json:"phase" enum:"parse,type,limit,lint" doc:"Where it arose: parse (syntax), type (an unknown name, a mismatched type, a result that is not the site's), limit (a cap), or lint (a warning or hint on an expression that compiles)"`
	Severity string             `json:"severity" enum:"error,warning,hint" doc:"error refuses the expression; warning (probably wrong) and hint (could be simpler) ride along with one that compiles and never refuse it"`
	Code     string             `json:"code,omitempty" required:"false" doc:"The registered diagnostic that gave it (sum-of-bools, float-equality, ...), stable across releases and documented on the expressions page; absent for the engine's own refusals"`
	Message  string             `json:"message"`
	Unbound  string             `json:"unbound,omitempty" required:"false" doc:"The name the expression uses that nothing binds, when that is the fault (an editor offers to bind it)"`
	Fix      *expressionFixBody `json:"fix,omitempty" required:"false" doc:"A quick fix the diagnostic offers: one text edit"`
}

// expressionFixBody is one text edit: the source from line:column up to
// (not including) end_line:end_column becomes replacement.
type expressionFixBody struct {
	Title       string `json:"title" doc:"What the fix does, as an action's label"`
	Line        int    `json:"line" doc:"The 1-based line the replaced range starts on"`
	Column      int    `json:"column" doc:"The 1-based column it starts at, in characters"`
	EndLine     int    `json:"end_line" doc:"The 1-based line it ends on"`
	EndColumn   int    `json:"end_column" doc:"The 1-based column it ends before"`
	Replacement string `json:"replacement" doc:"The text that replaces the range"`
}

func toFixBody(f *expression.Fix) *expressionFixBody {
	if f == nil {
		return nil
	}
	return &expressionFixBody{Title: f.Title, Line: f.Line, Column: f.Column, EndLine: f.EndLine, EndColumn: f.EndColumn, Replacement: f.Replacement}
}

type expressionBinding struct {
	Name string `json:"name" doc:"The name an expression uses (component.name for a nested field)"`
	Type string `json:"type" doc:"Its Go type"`
	Doc  string `json:"doc,omitempty" doc:"What it holds"`
}

type expressionBudget struct {
	Iterations int    `json:"iterations" doc:"The most a run may walk: the summed length of what its builtins and operators read, at least one per step"`
	Memory     int    `json:"memory" doc:"The most a run may allocate, in Expr's memory units"`
	Deadline   string `json:"deadline" doc:"The wall-clock backstop on a run's charged work, a Go duration"`
}

type validateExpressionOutput struct {
	Body struct {
		OK          bool                   `json:"ok" doc:"Whether the expression compiles at the site (no diagnostic of severity error)"`
		Site        string                 `json:"site"`
		ValueType   string                 `json:"value_type" enum:"number,text,bool" doc:"The value type the expression was checked against: for criteria the key's value, for calc the output's"`
		ResultType  string                 `json:"result_type" doc:"The type the site requires the expression to yield"`
		Diagnostics []expressionDiagnostic `json:"diagnostics" doc:"Why it does not compile (one error), or the warnings and hints it compiles with, each with its line and column; empty when it compiles clean"`
		Bindings    []expressionBinding    `json:"bindings" doc:"The names the expression may use at the site, generated from the site's environment"`
		Builtins    []string               `json:"builtins" doc:"The builtin functions the site admits"`
		Budget      expressionBudget       `json:"budget" doc:"What one run at the site may spend"`
	}
}

func registerExpressionRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "validate-expression",
		Method:      http.MethodPost,
		Path:        "/expressions:validate",
		Summary:     "Validate an expression",
		Description: "Compiles an expression against a registered site's environment and answers whether it compiles, with positioned diagnostics, and the site's bindings, admitted builtins and run budget, all generated from the site's declaration. For criteria a sample key is required, and types the value (a metric is a number; a property is text, or a bool for a bool property). For calc the key is the rule's output key, typing the result, and the rule's inputs (and owner_kind, component by default) give the bindings, each input's alias bound to the list of its key's type (a system also binds members, one record per member); an input that is unknown, or an alias that is not an identifier, is reserved or repeats, is a 422; a name the expression uses that no alias binds is a diagnostic carrying that name (unbound); the answer's value_type so reveals a key's lane and data type to a rule:read holder, as the rule list already does. Compiles without the engine's program cache and stores and runs nothing. An unknown site, a missing or unknown key is a 422. Gated by rule:read.",
	}, "rule", "read"), func(ctx context.Context, in *validateExpressionInput) (*validateExpressionOutput, error) {
		site, ok := rules.LookupSite(in.Body.Site)
		if !ok {
			return nil, huma.Error422UnprocessableEntity("site " + in.Body.Site + " is not a registered expression site")
		}
		if site.KeyRequired && in.Body.Key == "" {
			return nil, huma.Error422UnprocessableEntity("site " + site.Name + " needs the sample key the rule reads (key)")
		}
		valueType := site.Variants[0].ValueType
		if in.Body.Key != "" {
			lane, dataType, err := gw.SampleKeyType(ctx, in.Body.Key)
			if errors.Is(err, storage.ErrSampleKeyNotFound) {
				return nil, huma.Error422UnprocessableEntity("sample key " + in.Body.Key + " is not a registered property or metric")
			}
			if err != nil {
				return nil, huma.Error500InternalServerError("reading the sample key failed")
			}
			valueType = rules.ValueTypeFor(lane, dataType)
		}
		v, ok := site.Variant(valueType)
		if !ok {
			return nil, huma.Error500InternalServerError("the site has no environment for value type " + valueType)
		}
		env := v.Env
		if site.Bindings != "" {
			var err error
			if env, err = calcValidateEnv(ctx, gw, in.Body.OwnerKind, in.Body.Inputs, valueType); err != nil {
				return nil, err
			}
		}
		out := &validateExpressionOutput{}
		b := &out.Body
		b.Site, b.ValueType, b.ResultType = site.Name, valueType, env.Result().String()
		b.Diagnostics = []expressionDiagnostic{}
		check := expression.Check
		if site.Bindings != "" {
			check = rules.CheckCalc
		}
		diags, err := check(in.Body.Source, env)
		if err != nil {
			d := expressionDiagnostic{Phase: "type", Severity: string(expression.SeverityError), Message: err.Error()}
			var ee *expression.Error
			if errors.As(err, &ee) {
				d = expressionDiagnostic{Line: ee.Line, Column: ee.Column, Phase: string(ee.Phase), Severity: string(expression.SeverityError),
					Code: ee.Code, Message: ee.Message, Unbound: ee.Unbound, Fix: toFixBody(ee.Fix)}
			}
			b.Diagnostics = append(b.Diagnostics, d)
		}
		for _, d := range diags {
			b.Diagnostics = append(b.Diagnostics, expressionDiagnostic{Line: d.Line, Column: d.Column, Phase: "lint",
				Severity: string(d.Severity), Code: d.Code, Message: d.Message, Fix: toFixBody(d.Fix)})
		}
		b.OK = err == nil
		b.Bindings = []expressionBinding{}
		for _, x := range env.Bindings() {
			b.Bindings = append(b.Bindings, expressionBinding{Name: x.Name, Type: x.Type, Doc: x.Doc})
		}
		b.Builtins = env.Builtins()
		budget := env.Budget()
		b.Budget = expressionBudget{Iterations: budget.Iterations, Memory: budget.Memory, Deadline: budget.Deadline.String()}
		return out, nil
	})
}

// calcValidateEnv binds a calc rule's inputs for the validate route: each
// key resolved to its value type, the names and owner kind checked as an
// authored rule's are (a 422 naming the field when they are not).
func calcValidateEnv(ctx context.Context, gw storage.Gateway, ownerKind string, inputs []calcInputBody, output string) (expression.Env, error) {
	if ownerKind == "" {
		ownerKind = rules.OwnerComponent
	}
	typed := make([]rules.TypedInput, len(inputs))
	for i, x := range inputs {
		k, err := gw.ResolveSampleKey(ctx, x.Key)
		if errors.Is(err, storage.ErrSampleKeyNotFound) {
			return expression.Env{}, huma.Error422UnprocessableEntity("input key " + x.Key + " is not a registered property or metric")
		}
		if err != nil {
			return expression.Env{}, huma.Error500InternalServerError("reading the input key failed")
		}
		typed[i] = rules.TypedInput{Alias: x.Alias, Key: k.Name, KeyID: k.ID, Lane: k.Lane, ValueType: rules.ValueTypeFor(k.Lane, k.DataType),
			Window: x.WindowSeconds}
	}
	env, err := rules.CalcEnv(ownerKind, typed, output)
	var ce *rules.CalcError
	if errors.As(err, &ce) {
		return expression.Env{}, calcProblem(ce)
	}
	if err != nil {
		return expression.Env{}, huma.Error500InternalServerError("binding the calc inputs failed")
	}
	return env, nil
}
