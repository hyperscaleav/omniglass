package storage_test

import (
	"encoding/json"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A property a calc rule writes is the owner's effective value where nothing
// declares one, naming the rule by name and id, as the effective metric read
// does (#999's review): the console links the value to its rule. It is not a
// value anyone asked for, so reconciliation leaves it out, and a declared
// value still wins.
func TestCalcPropertyOutputIsEffective(t *testing.T) {
	r := newEvalRig(t)
	rule, err := r.create(storage.CalcRuleSpec{Name: "busy", OwnerKind: rules.OwnerComponent,
		Inputs: []rules.CalcInput{{Alias: "call", Key: "in-call"}}, Reduce: rules.CalcReduce{Expr: "any(call)"}, OutputKey: "busy-flag"})
	if err != nil {
		t.Fatal(err)
	}
	r.property("in-call", "true")
	r.drain()
	find := func() *storage.EffectiveProperty {
		t.Helper()
		effs, err := r.gw.EffectiveProperties(r.ctx, "component", r.comp, r.all)
		if err != nil {
			t.Fatal(err)
		}
		for i := range effs {
			if effs[i].PropertyTypeName == "busy-flag" {
				return &effs[i]
			}
		}
		return nil
	}
	got := find()
	if got == nil || string(got.Value) != "true" || got.SourceRule != "busy" || got.SourceRuleID != rule.ID || got.IsSet || got.FromContract {
		t.Fatalf("busy-flag resolves %+v, want true from the calc rule busy (%s), not set", got, rule.ID)
	}
	recs, err := r.gw.Reconciliation(r.ctx, "component", r.comp, r.all)
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range recs {
		if rec.PropertyTypeName == "busy-flag" {
			t.Fatalf("reconciliation lists the calculated busy-flag: %+v", rec)
		}
	}
	// The rollup's health is calculated too, by no calc rule: never listed.
	effs, err := r.gw.EffectiveProperties(r.ctx, "component", r.comp, r.all)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range effs {
		if e.PropertyTypeName == "health" {
			t.Fatalf("health resolves as a calculated property: %+v", e)
		}
	}
	// A value an operator declares wins, and names no rule.
	if _, err := r.gw.SetProperty(r.ctx, "", "component", r.comp, "busy-flag", "", json.RawMessage(`false`), r.all, r.all); err != nil {
		t.Fatal(err)
	}
	if got := find(); got == nil || string(got.Value) != "false" || got.SourceRuleID != "" || !got.IsSet {
		t.Fatalf("busy-flag with a declared value resolves %+v, want the declared false", got)
	}
}
