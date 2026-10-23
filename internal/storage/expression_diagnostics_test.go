package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/expression"
	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestStoredRuleDiagnostics proves the diagnostics registry (#1021) on
// stored rules: a warning never refuses a save and rides on every read as a
// non-blocking note; an error rule refuses at create and update alike; and a
// stored row that newly trips one reads it as its fault, with its code,
// without being rewritten.
func TestStoredRuleDiagnostics(t *testing.T) {
	r := newCalcRig(t)
	// A warning: an input the reduce never names.
	spec := storage.CalcRuleSpec{Name: "busy", OwnerKind: rules.OwnerComponent,
		Inputs: []rules.CalcInput{{Alias: "c", Key: "in-call"}, {Alias: "t", Key: "room-temperature"}},
		Reduce: rules.CalcReduce{Expr: "any(c)"}, OutputKey: "busy-flag"}
	got, err := r.create(spec)
	if err != nil {
		t.Fatalf("a warning refused the save: %v", err)
	}
	if got.CalcError != nil || len(got.CalcWarnings) != 1 || got.CalcWarnings[0].Code != "unused-alias" ||
		got.CalcWarnings[0].Field != "reduce.expr" || got.CalcWarnings[0].Message == "" {
		t.Fatalf("created %+v / %+v, want one unused-alias warning on reduce.expr", got.CalcError, got.CalcWarnings)
	}
	if g, err := r.gw.GetCalcRule(r.ctx, "busy"); err != nil || len(g.CalcWarnings) != 1 {
		t.Fatalf("get %+v (%v), want the warning", g, err)
	}
	if list, err := r.gw.ListCalcRules(r.ctx); err != nil || len(list) != 1 || len(list[0].CalcWarnings) != 1 {
		t.Fatalf("list %+v (%v), want the warning", list, err)
	}
	// An error rule refuses at create and at update, with its code.
	bad := spec
	bad.Name, bad.Reduce = "counts", rules.CalcReduce{Expr: "sum(c) > 0 && len(t) > 0"}
	_, err = r.create(bad)
	var ee *expression.Error
	if !errors.Is(err, storage.ErrCalcRuleInvalid) || !errors.As(err, &ee) || ee.Code != "sum-of-bools" {
		t.Fatalf("create: %v, want refused by sum-of-bools", err)
	}
	reduce := rules.CalcReduce{Expr: "sum(c) > 0 && len(t) > 0"}
	if _, err := r.gw.UpdateCalcRule(r.ctx, "", "busy", storage.CalcRulePatch{Reduce: &reduce}, r.all); !errors.As(err, &ee) || ee.Code != "sum-of-bools" {
		t.Fatalf("update: %v, want refused by sum-of-bools", err)
	}
	// A row that trips it (written before the rule existed, or outside the
	// gateway) reads it as its fault, and is not rewritten.
	if _, err := r.conn.Exec(r.ctx, `update calc_rule set reduce = '{"expr": "sum(c) > 0 && len(t) > 0"}', version = version + 1 where id = $1`, got.ID); err != nil {
		t.Fatal(err)
	}
	g, err := r.gw.GetCalcRule(r.ctx, "busy")
	if err != nil || g.CalcError == nil || g.CalcError.Code != "sum-of-bools" || g.CalcError.Line != 1 || g.CalcError.Column != 1 {
		t.Fatalf("the stored row reads %+v (%v), want calc_error sum-of-bools at 1:1", g, err)
	}
	if g.Reduce.Expr != "sum(c) > 0 && len(t) > 0" || len(g.CalcWarnings) != 0 {
		t.Fatalf("the stored row reads reduce %q, warnings %+v", g.Reduce.Expr, g.CalcWarnings)
	}

	// An event rule's warning: a float equality saves, and reads as a note
	// on its criteria.
	ctx, all := context.Background(), scope.Set{All: true}
	ev, err := r.gw.CreateEventRule(ctx, "", storage.EventRuleSpec{Name: "exactly-warm", SampleKey: "room-temperature",
		Fire: rules.Criteria{Expr: "value == 21.5"}, Clear: &rules.Criteria{Expr: "value < 20"}, FireEventType: "condition-started"}, all)
	if err != nil {
		t.Fatalf("a warning refused the event rule: %v", err)
	}
	if ev.CriteriaError != nil || len(ev.CriteriaWarnings) != 1 || ev.CriteriaWarnings[0].Code != "float-equality" ||
		ev.CriteriaWarnings[0].Field != "fire_criteria" || ev.CriteriaWarnings[0].Column != 7 {
		t.Fatalf("event rule %+v / %+v, want one float-equality warning on fire_criteria at column 7", ev.CriteriaError, ev.CriteriaWarnings)
	}
	if g, err := r.gw.GetEventRule(ctx, "exactly-warm", all); err != nil || len(g.CriteriaWarnings) != 1 {
		t.Fatalf("get %+v (%v), want the warning", g, err)
	}
	if list, err := r.gw.ListEventRules(ctx, all); err != nil || !hasWarnedRule(list, "exactly-warm") {
		t.Fatalf("list (%v) does not carry the warning", err)
	}
}

func hasWarnedRule(list []storage.EventRule, name string) bool {
	for _, r := range list {
		if r.Name == name {
			return len(r.CriteriaWarnings) == 1
		}
	}
	return false
}
