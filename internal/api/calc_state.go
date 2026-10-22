package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

type calcMemberBody struct {
	Key         string `json:"key" doc:"The input key the member held no value of"`
	Component   string `json:"component" doc:"The member component, by name"`
	ComponentID string `json:"component_id" doc:"The member component's uuid"`
}

type calcStaleBody struct {
	Key         string `json:"key" doc:"The input key"`
	Component   string `json:"component" doc:"The component the instance is on, by name"`
	ComponentID string `json:"component_id" doc:"The component's uuid"`
	Instance    string `json:"instance" doc:"The instance left out as stale"`
}

type calcOwnerStateBody struct {
	OwnerKind      string           `json:"owner_kind" enum:"component,system" doc:"What the owner is"`
	Owner          string           `json:"owner" doc:"The owner, by name, resolved now"`
	OwnerID        string           `json:"owner_id" doc:"The owner's uuid"`
	Value          json.RawMessage  `json:"value,omitempty" required:"false" doc:"The value the rule last computed on the owner; absent when it could not compute"`
	ComputedAt     *time.Time       `json:"computed_at,omitempty" required:"false" doc:"When the rule last computed on the owner; absent when it has not yet"`
	RuleVersion    int              `json:"rule_version,omitempty" required:"false" doc:"The rule version that computed it"`
	Missing        []string         `json:"missing" doc:"The input keys no value was found for (nothing was written)"`
	Skipped        []calcMemberBody `json:"skipped" doc:"A system rule's members that held no value of an input, those the caller may read"`
	SkippedHidden  int              `json:"skipped_hidden" doc:"How many more skipped members the caller may not read (counted, never named)"`
	Stale          []calcStaleBody  `json:"stale" doc:"The instances an input's list left out as stale, on components the caller may read"`
	StaleHidden    int              `json:"stale_hidden" doc:"How many more stale instances are on components the caller may not read"`
	Error          string           `json:"error,omitempty" required:"false" doc:"Why the rule could not compute (a run-time failure, a reduce that no longer compiles, a database fault it is backing off from)"`
	NoMembersSince *time.Time       `json:"no_members_since,omitempty" required:"false" doc:"A system with no role-bound members since then: the rule writes nothing and its last value stands, unsupported"`
	Pending        bool             `json:"pending" doc:"The owner is marked for the next tick"`
	DueAt          *time.Time       `json:"due_at,omitempty" required:"false" doc:"When a deferred mark (a failed computation backing off) is due"`
}

type calcRuleStateOutput struct {
	Body struct {
		Owners       []calcOwnerStateBody `json:"owners" doc:"The rule's state on each owner the caller may read, by owner name"`
		PendingDepth int                  `json:"pending_depth" doc:"How many of the owners listed are pending (only owners the caller may read are counted)"`
	}
}

func toMemberBodies(in []storage.CalcMemberRef) []calcMemberBody {
	out := make([]calcMemberBody, len(in))
	for i, m := range in {
		out[i] = calcMemberBody{Key: m.Key, Component: m.ComponentName, ComponentID: m.ComponentID}
	}
	return out
}

func toStaleBodies(in []storage.CalcStaleRef) []calcStaleBody {
	out := make([]calcStaleBody, len(in))
	for i, s := range in {
		out[i] = calcStaleBody{Key: s.Key, Component: s.ComponentName, ComponentID: s.ComponentID, Instance: s.Instance}
	}
	return out
}

type calcPreviewInput struct {
	Body struct {
		Name      string            `json:"name,omitempty" required:"false" doc:"A saved rule's name, to preview an edit of it (its stored version is then not a second writer of its key); absent for a new rule"`
		OwnerKind string            `json:"owner_kind" enum:"component,system" doc:"What the rule derives a value on"`
		Selector  *calcSelectorBody `json:"selector,omitempty" required:"false" doc:"The owners the rule derives for; absent is every owner of its kind"`
		Inputs    []calcInputBody   `json:"inputs" minItems:"1" maxItems:"16" doc:"The keys the rule reads, each by a unique alias its reduce uses"`
		Reduce    calcReduceBody    `json:"reduce" doc:"An expression over the inputs by alias"`
		OutputKey string            `json:"output_key" minLength:"1" doc:"The property or metric the rule would write"`
	}
}

type calcPreviewOwnerBody struct {
	OwnerKind     string           `json:"owner_kind" enum:"component,system" doc:"What the owner is"`
	Owner         string           `json:"owner" doc:"The owner, by name"`
	OwnerID       string           `json:"owner_id" doc:"The owner's uuid"`
	Value         json.RawMessage  `json:"value,omitempty" required:"false" doc:"The value the rule would write now; absent when it would write nothing"`
	Current       json.RawMessage  `json:"current,omitempty" required:"false" doc:"The output key's current value on the owner; absent when it has none"`
	Differs       bool             `json:"differs" doc:"Whether the value differs from the current one, so the rule would write a sample"`
	Missing       []string         `json:"missing" doc:"The input keys no value was found for"`
	Skipped       []calcMemberBody `json:"skipped" doc:"A system rule's members that held no value of an input, those the caller may read"`
	SkippedHidden int              `json:"skipped_hidden" doc:"How many more skipped members the caller may not read"`
	Stale         []calcStaleBody  `json:"stale" doc:"The instances an input's list would leave out as stale, on components the caller may read"`
	StaleHidden   int              `json:"stale_hidden" doc:"How many more stale instances the caller may not read"`
	NoMembers     bool             `json:"no_members" doc:"A system with no role-bound members: the rule would write nothing"`
	Error         string           `json:"error,omitempty" required:"false" doc:"Why the rule could not compute on the owner"`
}

type calcPreviewOutput struct {
	Body struct {
		Owners       []calcPreviewOwnerBody `json:"owners" doc:"What the rule would do on each owner it selects that the caller may read, by owner id"`
		OwnersJudged int                    `json:"owners_judged" doc:"How many owners were computed"`
		OwnersTotal  int                    `json:"owners_total" doc:"How many owners the rule selects that the caller may read"`
		OwnerCap     int                    `json:"owner_cap" doc:"The most owners one preview computes"`
		Truncated    bool                   `json:"truncated" doc:"The cap stopped the preview short of every owner"`
	}
}

// registerCalcRuleStateRoutes wires a calc rule's live state and its
// preview (#998), both rule:read and fenced to the caller's component and
// system read scopes.
func registerCalcRuleStateRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-calc-rule-state",
		Method:      http.MethodGet,
		Path:        "/calc-rules/{name}/state",
		Summary:     "Get a calc rule's live state",
		Description: "What a calc rule last did on each owner the caller may read (components by the component read scope, systems by the system read scope), each owner named now: the value it computed, when and at which version, the inputs it found no value of, a system's members that held none and the instances it left out as stale (named when the caller may read the component, else counted), an error, since when a system has had no members, and whether the owner is pending (with when a deferred mark is due). The pending depth counts the listed owners that are pending. Gated by rule:read.",
	}, "rule", "read"), func(ctx context.Context, in *calcRuleNameInput) (*calcRuleStateOutput, error) {
		live, err := gw.CalcRuleState(ctx, in.Name, a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "system", "read"))
		if err != nil {
			return nil, mapCalcRuleErr(err)
		}
		out := &calcRuleStateOutput{}
		out.Body.PendingDepth = live.PendingDepth
		out.Body.Owners = make([]calcOwnerStateBody, 0, len(live.Owners))
		for _, o := range live.Owners {
			missing := o.Missing
			if missing == nil {
				missing = []string{}
			}
			out.Body.Owners = append(out.Body.Owners, calcOwnerStateBody{OwnerKind: o.OwnerKind, Owner: o.OwnerName, OwnerID: o.OwnerID,
				Value: o.Value, ComputedAt: o.ComputedAt, RuleVersion: o.RuleVersion, Missing: missing,
				Skipped: toMemberBodies(o.Skipped), SkippedHidden: o.SkippedHidden, Stale: toStaleBodies(o.Stale), StaleHidden: o.StaleHidden,
				Error: o.Error, NoMembersSince: o.NoMembersSince, Pending: o.Pending, DueAt: o.DueAt})
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "preview-calc-rule",
		Method:      http.MethodPost,
		Path:        "/calc-rules:preview",
		Summary:     "Preview a calc rule",
		Description: "Computes a candidate calc rule now, on each owner it selects that the caller may read, with the reduce the tick runs, and writes nothing (a read-only transaction). For each owner it answers the value the rule would write, the output's current value and whether they differ, the inputs with no value, the members skipped and the instances left out as stale (named when the caller may read the component, else counted). An expression that reads one member's value by position (members[1].value.t, or an element of an input's list) over a system with a member the caller may not read is withheld on that system, its error counting the members out of scope; one that folds each input's list whole (avg(t), max(t)) shows the aggregate. A component rule previews the selected components holding one of its inputs, as the tick would mark them. At most owner_cap owners are computed, and a bound on total input reads stops a preview over large systems early, a whole owner at a time (owners_judged of owners_total, truncated past either). The candidate is checked as a save would check it: a reduce that does not compile, a cycle in the key graph, an output another enabled rule or a collection writes, or health are each a 422, so the author learns before saving; given a saved rule's name it is judged as an edit of that rule. The selector's names resolve in the caller's read scope: a name that does not exist and one the caller may not read are the same 422, and a name shared in and out of scope means the one in scope. A preview takes no lock a save waits on. Gated by rule:read.",
	}, "rule", "read"), func(ctx context.Context, in *calcPreviewInput) (*calcPreviewOutput, error) {
		b := in.Body
		spec := storage.CalcRuleSpec{Name: b.Name, OwnerKind: b.OwnerKind, Inputs: fromCalcInputs(b.Inputs),
			Reduce: rules.CalcReduce{Expr: b.Reduce.Expr}, OutputKey: b.OutputKey}
		if b.Selector != nil {
			spec.Selector = b.Selector.toStorage()
		}
		pv, err := gw.PreviewCalcRule(ctx, spec, a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "system", "read"))
		if err != nil {
			return nil, mapCalcRuleErr(err)
		}
		out := &calcPreviewOutput{}
		out.Body.OwnersJudged, out.Body.OwnersTotal, out.Body.OwnerCap, out.Body.Truncated = pv.OwnersJudged, pv.OwnersTotal, pv.OwnerCap, pv.Truncated
		out.Body.Owners = make([]calcPreviewOwnerBody, 0, len(pv.Owners))
		for _, o := range pv.Owners {
			out.Body.Owners = append(out.Body.Owners, calcPreviewOwnerBody{OwnerKind: o.OwnerKind, Owner: o.OwnerName, OwnerID: o.OwnerID,
				Value: o.Value, Current: o.Current, Differs: o.Differs, Missing: o.Missing, Skipped: toMemberBodies(o.Skipped),
				SkippedHidden: o.SkippedHidden, Stale: toStaleBodies(o.Stale), StaleHidden: o.StaleHidden, NoMembers: o.NoMembers, Error: o.Error})
		}
		return out, nil
	})
}
