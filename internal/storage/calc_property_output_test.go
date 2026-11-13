package storage_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A property a calc rule writes is the owner's effective value where nothing
// declares one, naming the rule by name and id, as the effective metric read
// does (#999's review): the console links the value to its rule. It is not a
// value anyone asked for, so its row carries no reconciliation, and a declared
// value still wins.
func TestCalcPropertyOutputIsEffective(t *testing.T) {
	t.Parallel()
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
	if got.Reconciliation != nil {
		t.Fatalf("the calculated busy-flag carries a reconciliation: %+v", got.Reconciliation)
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

// A property a calc rule writes passes the same value check ingest applies
// (key.ValidateValue against the property's data type and validation): a
// result the property cannot hold is the rule's error on that owner, never a
// sample, and not a database fault, so the mark is not deferred for a retry
// that cannot succeed.
func TestCalcPropertyOutputPassesTheValueCheck(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, output, dataType, validation, input, wantSample, wantErr string
	}{
		{"json schema refuses text", "seat-map", "json", `{"type":"object"}`, "hdmi1", "", "seat-map"},
		{"enum refuses outside", "input-mode", "string", `{"enum":["hdmi1","hdmi2"]}`, "usb", "", "input-mode"},
		{"enum holds inside", "input-mode", "string", `{"enum":["hdmi1","hdmi2"]}`, "hdmi2", "hdmi2", ""},
		{"pattern refuses", "room-code", "string", `{"pattern":"^R[0-9]+$"}`, "lobby", "", "room-code"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newEvalRig(t)
			spec := storage.PropertyTypeSpec{Name: tc.output, DataType: tc.dataType}
			if tc.validation != "" {
				spec.Validation = []byte(tc.validation)
			}
			if _, err := r.gw.CreatePropertyType(r.ctx, "", spec); err != nil {
				t.Fatal(err)
			}
			rule, err := r.create(storage.CalcRuleSpec{Name: "pick", OwnerKind: rules.OwnerComponent,
				Inputs: []rules.CalcInput{{Alias: "v", Key: "video-input"}}, Reduce: rules.CalcReduce{Expr: "majority(v)"}, OutputKey: tc.output})
			if err != nil {
				t.Fatal(err)
			}
			r.property("video-input", tc.input)
			r.drain()
			got := scalar(t, r.conn, fmt.Sprintf(`select coalesce(string_agg(value #>> '{}', ','), '') from property
				where component_id = '%s' and property_type_id = (select id from property_type where name = '%s') and provenance = 'calculated'`, r.comp, tc.output))
			if got != tc.wantSample {
				t.Fatalf("%s samples %q, want %q", tc.output, got, tc.wantSample)
			}
			live, err := r.gw.CalcRuleState(r.ctx, rule.ID, r.all, r.all, 10)
			if err != nil {
				t.Fatal(err)
			}
			if len(live.Owners) != 1 {
				t.Fatalf("%d owner states, want 1", len(live.Owners))
			}
			o := live.Owners[0]
			if tc.wantErr == "" {
				if o.Error != "" {
					t.Fatalf("state error %q on a valid value", o.Error)
				}
				return
			}
			if !strings.Contains(o.Error, tc.wantErr) {
				t.Fatalf("state error %q, want it to name %s and the refusal", o.Error, tc.wantErr)
			}
			if o.Pending || o.DueAt != nil || r.pending() != 0 {
				t.Fatalf("refusal deferred the mark: pending=%v due=%v marks=%d", o.Pending, o.DueAt, r.pending())
			}
		})
	}
}
