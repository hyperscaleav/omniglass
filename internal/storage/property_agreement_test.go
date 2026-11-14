package storage_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// #1202: an observed property value is stored as the JSON string of its text
// form (the node lane's propertyText), a declared or intended one as typed
// jsonb. Two values that mean the same typed value must agree to every
// reader that compares the provenances: reconciliation's drift and a
// settleable command's verdict.

// land lands one observed text on the display through the transition-only
// ingest path every lane's samples take, so a repeat is held back.
func (r *settleRig) land(t *testing.T, key, text string) {
	t.Helper()
	_, err := r.gw.LandBatch(r.ctx, storage.IngestBatch{
		BatchID: fmt.Sprintf("agree-%s-%d", key, time.Now().UnixNano()),
		Writer:  storage.PushWriter(),
		Properties: []storage.PropertySampleWrite{{
			OwnerKind: "component", OwnerID: "display", Key: key, Instance: "rx-1", Value: text, Source: "push",
		}},
	})
	if err != nil {
		t.Fatalf("land %s=%s: %v", key, text, err)
	}
}

// reconciliationOf declares want on the display (nil declares nothing), lands
// observed (the text the node lane stores) on the one instance, and returns
// the key's reconciliation.
func (r *settleRig) reconciliationOf(t *testing.T, key, dataType, want, observed string) *storage.PropertyReconciliation {
	t.Helper()
	all := scope.Set{All: true}
	if _, err := r.gw.CreatePropertyType(r.ctx, "", storage.PropertyTypeSpec{Name: key, DataType: dataType}); err != nil {
		t.Fatalf("create %s: %v", key, err)
	}
	if _, err := r.gw.SetProperty(r.ctx, "", "component", "display", key, "", json.RawMessage(want), all, all); err != nil {
		t.Fatalf("declare %s=%s: %v", key, want, err)
	}
	r.land(t, key, observed)
	for _, e := range mustResolveOwner(t, r.gw, "component", "display", all) {
		if e.PropertyTypeName == key {
			if e.Reconciliation == nil {
				t.Fatalf("%s carries no reconciliation: %+v", key, e)
			}
			return e.Reconciliation
		}
	}
	t.Fatalf("%s is missing from the display's properties", key)
	return nil
}

func TestReconciliationAgreesOnTheSameTypedValue(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, dataType, want, observed string
		drift                          bool
	}{
		{"bool agrees", "bool", `true`, `true`, false},
		{"bool differs", "bool", `true`, `false`, true},
		{"int agrees", "json", `3`, `3`, false},
		{"int differs", "json", `3`, `4`, true},
		{"number agrees across spelling", "json", `3.0`, `3`, false},
		{"object agrees across key order and spacing", "json", `{"b": 1, "a": [1, 2]}`, `{"a":[1,2],"b":1}`, false},
		{"string agrees", "string", `"hdmi1"`, `hdmi1`, false},
		{"string differs", "string", `"hdmi1"`, `hdmi2`, true},
		{"a numeric-looking string is compared as text", "string", `"1.10"`, `1.1`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newSettleRig(t, "")
			r.ownerLevelDisplay(t)
			rec := r.reconciliationOf(t, "agree-probe", tc.dataType, tc.want, tc.observed)
			if rec.Drift != tc.drift {
				t.Fatalf("drift = %v, want %v (want %s, is %s)", rec.Drift, tc.drift, rec.Want, rec.Is)
			}
			if !tc.drift && string(rec.Is) != string(rec.Want) && tc.dataType == "bool" {
				t.Fatalf("is = %s, want it to read as the declared %s", rec.Is, rec.Want)
			}
		})
	}
}

// A settleable command whose target is a bool property settles on the
// observation of the same value, and not on the other one.
func TestABoolCommandSettlesOnTheObservedBool(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, observed, status string
	}{
		{"same value settles", "true", "settled"},
		{"other value leaves it pending", "false", "issued"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newSettleRig(t, "")
			r.ownerLevelDisplay(t)
			if _, err := r.gw.CreatePropertyType(r.ctx, "", storage.PropertyTypeSpec{Name: "panel-lit", DataType: "bool"}); err != nil {
				t.Fatal(err)
			}
			if _, err := r.gw.CreateCommandType(r.ctx, "", storage.CommandTypeSpec{
				Name: "set-lit", Label: "Set lit", TargetPropertyType: "panel-lit", SettleWindowSeconds: 60,
			}); err != nil {
				t.Fatalf("create command type: %v", err)
			}
			cmd, err := r.gw.IssueCommand(r.ctx, r.actor, "component", "display", "set-lit", "", json.RawMessage(`true`), nil)
			if err != nil {
				t.Fatalf("issue: %v", err)
			}
			r.land(t, "panel-lit", tc.observed)
			if got := r.status(t, cmd.ID); got != tc.status {
				t.Fatalf("status = %q, want %q", got, tc.status)
			}
			verdict, err := r.gw.CommandSettlement(r.ctx, "component", "display", "set-lit", "")
			if err != nil {
				t.Fatal(err)
			}
			if (verdict == storage.SettlementSettled) != (tc.status == "settled") {
				t.Fatalf("computed verdict = %q, recorded status %q", verdict, tc.status)
			}
		})
	}
}

// The transition-only guard still suppresses a repeated identical sample.
func TestARepeatedBoolObservationIsOneRow(t *testing.T) {
	t.Parallel()
	r := newSettleRig(t, "")
	r.ownerLevelDisplay(t)
	if _, err := r.gw.CreatePropertyType(r.ctx, "", storage.PropertyTypeSpec{Name: "panel-lit", DataType: "bool"}); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"true", "true", "false", "false"} {
		r.land(t, "panel-lit", v)
	}
	var n int
	if err := r.conn.QueryRow(r.ctx, `select count(*) from property where provenance = 'observed'
		and property_type_id = (select id from property_type where name = 'panel-lit')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("%d observed rows, want 2 (one per transition)", n)
	}
}
