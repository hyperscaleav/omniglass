package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/expression"
	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

type calcInputBody struct {
	As  string `json:"as" minLength:"1" maxLength:"64" pattern:"^[A-Za-z_][A-Za-z0-9_]*$" doc:"The name the reduce knows the input by: an identifier, not a keyword or a builtin's name"`
	Key string `json:"key" minLength:"1" doc:"The property or metric the input reads, by name"`
}

type calcReduceBody struct {
	Reducer string `json:"reducer,omitempty" required:"false" enum:"average,min,max,sum,count,majority,any,all" doc:"A named fold over every value of the rule's one input (every instance on a component; every instance on every role-bound member of a system); give this or expr"`
	Expr    string `json:"expr,omitempty" required:"false" doc:"An expression over the inputs by their names, yielding the output key's type; give this or reducer"`
}

type calcRuleBody struct {
	ID          string          `json:"id" doc:"The rule's uuid, the stable form of name"`
	Name        string          `json:"name" doc:"The rule's name, the address an operator types; renameable"`
	Label       string          `json:"label,omitempty" doc:"A human label"`
	Description string          `json:"description,omitempty" doc:"What the rule derives and why"`
	OwnerKind   string          `json:"owner_kind" enum:"component,system" doc:"What the rule derives a value on: each component its selector picks, or each system"`
	Selector    selectorBody    `json:"selector" doc:"The components a component rule derives for; both lists empty is every component. A system rule's is empty"`
	Inputs      []calcInputBody `json:"inputs" doc:"The keys the rule reads, each by the name its reduce uses"`
	Reduce      calcReduceBody  `json:"reduce" doc:"How the inputs fold into the output: a named reducer or an expression"`
	OutputKey   string          `json:"output_key" doc:"The property or metric the rule writes"`
	OutputLane  string          `json:"output_lane" enum:"property,metric" doc:"Which lane the output key is on, derived from the registries"`
	Enabled     bool            `json:"enabled" doc:"Whether the rule runs; an enabled rule is part of the key graph"`
	Official    bool            `json:"official" doc:"Shipped with the platform and read-only"`
	Version     int             `json:"version" doc:"Bumps on every update"`
	CreatedAt   time.Time       `json:"created_at" doc:"When the rule was created"`
	UpdatedAt   time.Time       `json:"updated_at" doc:"When the rule was last written"`
	CalcError   *calcErrorBody  `json:"calc_error,omitempty" required:"false" doc:"Why the stored reduce does not compile against the registries as they are now (an input key deleted or retyped); absent when it compiles. While present the rule cannot be enabled"`
}

// calcErrorBody is a stored rule's compile error.
type calcErrorBody struct {
	Field   string `json:"field" doc:"The field at fault: an input's key (inputs[0].key), reduce.expr, reduce.reducer or output_key"`
	Line    int    `json:"line,omitempty" required:"false" doc:"The 1-based line of the fault, when the reduce's expression is what failed"`
	Column  int    `json:"column,omitempty" required:"false" doc:"The 1-based column of the fault, when it has a line"`
	Message string `json:"message" doc:"Why it does not compile"`
}

func toCalcRuleBody(r *storage.CalcRule) calcRuleBody {
	b := calcRuleBody{ID: r.ID, Name: r.Name, Label: r.Label, Description: r.Description, OwnerKind: r.OwnerKind,
		Selector:  selectorBody{ComponentTypes: r.Selector.ComponentTypes, Components: r.Selector.Components},
		Reduce:    calcReduceBody{Reducer: r.Reduce.Reducer, Expr: r.Reduce.Expr},
		OutputKey: r.OutputKey, OutputLane: string(r.OutputLane), Enabled: r.Enabled, Official: r.Official, Version: r.Version,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
	if f := r.CalcError; f != nil {
		b.CalcError = &calcErrorBody{Field: f.Field, Line: f.Line, Column: f.Column, Message: f.Message}
	}
	b.Inputs = make([]calcInputBody, len(r.Inputs))
	for i, in := range r.Inputs {
		b.Inputs[i] = calcInputBody{As: in.As, Key: in.Key}
	}
	return b
}

func fromCalcInputs(in []calcInputBody) []rules.CalcInput {
	out := make([]rules.CalcInput, len(in))
	for i, x := range in {
		out[i] = rules.CalcInput{As: x.As, Key: x.Key}
	}
	return out
}

type listCalcRulesOutput struct {
	Body struct {
		CalcRules []calcRuleBody `json:"calc_rules" doc:"Every calc rule, by name"`
	}
}

type calcRuleOutput struct{ Body calcRuleBody }

type calcRuleNameInput struct {
	Name string `path:"name" doc:"The rule's name (its uuid is accepted too)"`
}

type createCalcRuleInput struct {
	Body struct {
		Name        string          `json:"name" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"The rule's name (lowercase letters, digits, hyphens)"`
		Label       string          `json:"label,omitempty" required:"false" doc:"A human label"`
		Description string          `json:"description,omitempty" required:"false" doc:"What the rule derives and why"`
		OwnerKind   string          `json:"owner_kind" enum:"component,system" doc:"What the rule derives a value on: each component its selector picks, or each system. Fixed at create"`
		Selector    *selectorBody   `json:"selector,omitempty" required:"false" doc:"The components a component rule derives for (component types and components); absent is every component. A system rule takes none"`
		Inputs      []calcInputBody `json:"inputs" minItems:"1" maxItems:"16" doc:"The keys the rule reads, each by a unique name its reduce uses; health may not be one"`
		Reduce      calcReduceBody  `json:"reduce" doc:"A named reducer over the one input, or an expression over the inputs by name"`
		OutputKey   string          `json:"output_key" minLength:"1" doc:"The property or metric the rule writes: its type pins the reduce's result. Not health, not a key a collection writes, and not one another enabled rule writes"`
		Enabled     *bool           `json:"enabled,omitempty" required:"false" doc:"Defaults to true"`
	}
}

type updateCalcRuleInput struct {
	Name string `path:"name" doc:"The rule's name (its uuid is accepted too)"`
	Body struct {
		Label       *string          `json:"label,omitempty" required:"false" doc:"A human label"`
		Description *string          `json:"description,omitempty" required:"false" doc:"What the rule derives and why"`
		Selector    *selectorBody    `json:"selector,omitempty" required:"false" doc:"Replace the components the rule derives for; an empty selector is every component"`
		Inputs      *[]calcInputBody `json:"inputs,omitempty" required:"false" minItems:"1" maxItems:"16" doc:"Replace the keys the rule reads"`
		Reduce      *calcReduceBody  `json:"reduce,omitempty" required:"false" doc:"Replace the reduce"`
		OutputKey   *string          `json:"output_key,omitempty" required:"false" doc:"The property or metric the rule writes"`
		Enabled     *bool            `json:"enabled,omitempty" required:"false" doc:"Whether the rule runs; an enabled rule is part of the key graph"`
	}
}

// renameCalcRuleInput is the :rename body, its name rule in the contract as
// well as in the gateway's validator (as for event rules).
type renameCalcRuleInput struct {
	Name string `path:"name" doc:"The rule's current name (its uuid is accepted too)"`
	Body struct {
		Name string `json:"name" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"The rule's new name, unique among calc rules (lowercase letters, digits, hyphens)"`
	}
}

// registerCalcRuleRoutes wires calc rule authoring (#995, epic #525): rules
// that derive an output key on each owner from named inputs, gated rule:*
// like event rules (ADR-0144 ruling 2). A rule reads like a registry
// (rule:read from any grant); writing one takes an all-scope grant, since a
// rule reaches the whole fleet. Official rules are read-only.
func registerCalcRuleRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-calc-rule",
		Method:      http.MethodGet,
		Path:        "/calc-rules",
		Summary:     "List calc rules",
		Description: "Lists the calc rules, by name: the authored config that derives an output key on each owner from named inputs. Rules read like a registry, so every rule is listed whatever the caller's scope. Gated by rule:read.",
	}, "rule", "read"), func(ctx context.Context, _ *struct{}) (*listCalcRulesOutput, error) {
		rs, err := gw.ListCalcRules(ctx)
		if err != nil {
			return nil, mapCalcRuleErr(err)
		}
		out := &listCalcRulesOutput{}
		out.Body.CalcRules = make([]calcRuleBody, 0, len(rs))
		for i := range rs {
			out.Body.CalcRules = append(out.Body.CalcRules, toCalcRuleBody(&rs[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-calc-rule",
		Method:      http.MethodGet,
		Path:        "/calc-rules/{name}",
		Summary:     "Get a calc rule",
		Description: "Returns one calc rule by name (or uuid). Gated by rule:read.",
	}, "rule", "read"), func(ctx context.Context, in *calcRuleNameInput) (*calcRuleOutput, error) {
		r, err := gw.GetCalcRule(ctx, in.Name)
		if err != nil {
			return nil, mapCalcRuleErr(err)
		}
		return &calcRuleOutput{Body: toCalcRuleBody(r)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-calc-rule",
		Method:        http.MethodPost,
		Path:          "/calc-rules",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a calc rule",
		Description:   "Authors a rule that derives its output key on each owner (each component its selector picks, or each system) from its named inputs, folded by a named reducer (one input) or an expression over the inputs by name. Every input key must be registered and health may not be one; the reduce must compile and yield the output key's type (a 422 placed at body.reduce.expr with its line and column); the output must be a registered key that is not health and that no collection writes (a native probe's key, or one a driver declares among its emits); an enabled rule may not close a cycle in the graph from every enabled rule's input keys to its output key (a 422 naming the chain of keys), nor write a key another enabled rule writes (a 422 naming that rule). A list or get carries calc_error when a stored reduce no longer compiles. A name already taken is a 409. Returns the rule with its uuid. Audited. Gated by rule:create, with an all-scope grant.",
	}, "rule", "create"), func(ctx context.Context, in *createCalcRuleInput) (*calcRuleOutput, error) {
		b := in.Body
		spec := storage.CalcRuleSpec{Name: b.Name, Label: b.Label, Description: b.Description, OwnerKind: b.OwnerKind,
			Inputs: fromCalcInputs(b.Inputs), Reduce: rules.CalcReduce{Reducer: b.Reduce.Reducer, Expr: b.Reduce.Expr},
			OutputKey: b.OutputKey, Enabled: b.Enabled}
		if b.Selector != nil {
			spec.Selector = storage.EventRuleSelector{ComponentTypes: b.Selector.ComponentTypes, Components: b.Selector.Components}
		}
		r, err := gw.CreateCalcRule(ctx, actorID(ctx), spec, a.scopeFor(ctx, "rule", "create"))
		if err != nil {
			return nil, mapCalcRuleErr(err)
		}
		return &calcRuleOutput{Body: toCalcRuleBody(r)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-calc-rule",
		Method:      http.MethodPatch,
		Path:        "/calc-rules/{name}",
		Summary:     "Update a calc rule",
		Description: "Patches a rule (an absent field is unchanged), bumping its version; a patch that changes nothing writes nothing. What changed is checked as on create: changed inputs, reduce or output recompile, a changed output must be one no collection writes, and an enabled rule may not close a cycle or share its output with another enabled rule. Enabling a disabled rule recompiles it and checks both, so a rule carrying calc_error cannot be enabled (422). The owner kind is fixed at create. Official rules are read-only (409). Audited. Gated by rule:update, with an all-scope grant.",
	}, "rule", "update"), func(ctx context.Context, in *updateCalcRuleInput) (*calcRuleOutput, error) {
		b := in.Body
		patch := storage.CalcRulePatch{Label: b.Label, Description: b.Description, OutputKey: b.OutputKey, Enabled: b.Enabled}
		if b.Selector != nil {
			patch.Selector = &storage.EventRuleSelector{ComponentTypes: b.Selector.ComponentTypes, Components: b.Selector.Components}
		}
		if b.Inputs != nil {
			inputs := fromCalcInputs(*b.Inputs)
			patch.Inputs = &inputs
		}
		if b.Reduce != nil {
			patch.Reduce = &rules.CalcReduce{Reducer: b.Reduce.Reducer, Expr: b.Reduce.Expr}
		}
		r, err := gw.UpdateCalcRule(ctx, actorID(ctx), in.Name, patch, a.scopeFor(ctx, "rule", "update"))
		if err != nil {
			return nil, mapCalcRuleErr(err)
		}
		return &calcRuleOutput{Body: toCalcRuleBody(r)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "rename-calc-rule",
		Method:      http.MethodPost,
		Path:        "/calc-rules/{name}:rename",
		Summary:     "Rename a calc rule",
		Description: "Moves an operator rule's name, the address an operator types. A separate act from an update, because it breaks bookmarks and integration config outside this system; inside it nothing breaks, since the audit rows hold the rule's uuid and the key graph is of keys, not rule names. Not a material change: no version bump. An official rule keeps its name (409), a taken name is a 409, an illegal or uuid-shaped one a 422. Returns the rule with its uuid. Audited. Gated by rule:update, with an all-scope grant.",
	}, "rule", "update"), func(ctx context.Context, in *renameCalcRuleInput) (*calcRuleOutput, error) {
		r, err := gw.RenameCalcRule(ctx, actorID(ctx), in.Name, in.Body.Name, a.scopeFor(ctx, "rule", "update"))
		if err != nil {
			return nil, mapCalcRuleErr(err)
		}
		return &calcRuleOutput{Body: toCalcRuleBody(r)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-calc-rule",
		Method:        http.MethodDelete,
		Path:          "/calc-rules/{name}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a calc rule",
		Description:   "Removes an operator rule; an official rule is read-only (409). Audited. Gated by rule:delete, with an all-scope grant.",
	}, "rule", "delete"), func(ctx context.Context, in *calcRuleNameInput) (*struct{}, error) {
		if err := gw.DeleteCalcRule(ctx, actorID(ctx), in.Name, a.scopeFor(ctx, "rule", "delete")); err != nil {
			return nil, mapCalcRuleErr(err)
		}
		return nil, nil
	})
}

// calcProblem is a 422 for a rule refused on one field, located at that
// field in the body, carrying the line and column when a reduce's expression
// is what failed.
func calcProblem(ce *rules.CalcError) error {
	d := &huma.ErrorDetail{Location: "body." + ce.Field, Message: ce.Err.Error()}
	var ee *expression.Error
	if errors.As(ce, &ee) {
		d.Message = ee.Message
		if ee.Line > 0 {
			d.Value = map[string]any{"line": ee.Line, "column": ee.Column, "phase": string(ee.Phase)}
		}
	}
	return huma.Error422UnprocessableEntity(ce.Field+": "+d.Message, d)
}

func mapCalcRuleErr(err error) error {
	if refErr, ok := mapRefErr(err); ok {
		return refErr
	}
	var cyc *storage.CalcCycleError
	switch {
	case errors.Is(err, storage.ErrCalcRuleNotFound):
		return huma.Error404NotFound("calc rule not found")
	case errors.Is(err, storage.ErrCalcRuleOfficial):
		return huma.Error409Conflict("an official calc rule is read-only")
	case errors.Is(err, storage.ErrCalcRuleExists):
		return huma.Error409Conflict("a calc rule already has that name")
	case errors.Is(err, storage.ErrCalcRuleForbidden):
		return huma.Error403Forbidden("writing a calc rule needs an all-scope grant: a rule reaches the whole fleet")
	case errors.As(err, &cyc):
		return huma.Error422UnprocessableEntity("the rule closes a cycle in the calc key graph: "+cyc.Cycle.String(),
			&huma.ErrorDetail{Location: "body.inputs", Message: "closes the cycle " + cyc.Cycle.String(),
				Value: map[string]any{"keys": cyc.Cycle.Keys, "rules": cyc.Cycle.Rules}})
	case errors.Is(err, storage.ErrCalcRuleInvalid):
		var ce *rules.CalcError
		if errors.As(err, &ce) {
			return calcProblem(ce)
		}
		return huma.Error422UnprocessableEntity(err.Error())
	}
	return huma.Error500InternalServerError("calc rule operation failed")
}
