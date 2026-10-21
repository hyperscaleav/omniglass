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
		Site   string `json:"site" enum:"criteria" doc:"Where the expression is written: criteria, an event rule's fire_criteria or clear_criteria"`
		Source string `json:"source" doc:"The expression"`
		Key    string `json:"key,omitempty" required:"false" doc:"For criteria (required there), the sample key the rule reads: its lane and data type type the value (a metric is a number; a property is text, or a bool for a bool property)"`
	}
}

type expressionDiagnostic struct {
	Line    int    `json:"line" doc:"The 1-based line of the fault; 0 when it has none (a length cap)"`
	Column  int    `json:"column" doc:"The 1-based column of the fault; 0 when it has none"`
	Phase   string `json:"phase" enum:"parse,type,limit" doc:"Where it failed: parse (syntax), type (an unknown name, a mismatched type, a result that is not the site's), or limit (a cap)"`
	Message string `json:"message"`
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
		OK          bool                   `json:"ok" doc:"Whether the expression compiles at the site"`
		Site        string                 `json:"site"`
		ValueType   string                 `json:"value_type" enum:"number,text,bool" doc:"The value type the expression was checked against (criteria)"`
		ResultType  string                 `json:"result_type" doc:"The type the site requires the expression to yield"`
		Diagnostics []expressionDiagnostic `json:"diagnostics" doc:"Why it does not compile, with line and column; empty when ok"`
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
		Description: "Compiles an expression against a registered site's environment and answers whether it compiles, with positioned diagnostics, and the site's bindings, admitted builtins and run budget, all generated from the site's declaration. For criteria a sample key is required, and types the value (a metric is a number; a property is text, or a bool for a bool property); the answer's value_type so reveals a key's lane and data type to a rule:read holder, as the rule list already does. Compiles without the engine's program cache and stores and runs nothing. An unknown site, a missing or unknown key is a 422. Gated by rule:read.",
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
		out := &validateExpressionOutput{}
		b := &out.Body
		b.Site, b.ValueType, b.ResultType = site.Name, valueType, v.Env.Result().String()
		b.Diagnostics = []expressionDiagnostic{}
		if err := expression.Check(in.Body.Source, v.Env); err != nil {
			d := expressionDiagnostic{Phase: "type", Message: err.Error()}
			var ee *expression.Error
			if errors.As(err, &ee) {
				d = expressionDiagnostic{Line: ee.Line, Column: ee.Column, Phase: string(ee.Phase), Message: ee.Message}
			}
			b.Diagnostics = append(b.Diagnostics, d)
		}
		b.OK = len(b.Diagnostics) == 0
		for _, x := range v.Env.Bindings() {
			b.Bindings = append(b.Bindings, expressionBinding{Name: x.Name, Type: x.Type, Doc: x.Doc})
		}
		b.Builtins = v.Env.Builtins()
		budget := v.Env.Budget()
		b.Budget = expressionBudget{Iterations: budget.Iterations, Memory: budget.Memory, Deadline: budget.Deadline.String()}
		return out, nil
	})
}
