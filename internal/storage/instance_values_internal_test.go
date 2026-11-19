package storage

import (
	"encoding/json"
	"testing"
	"time"
)

// instanceValues builds a newest-first instance list from (instance, value) pairs.
func instanceValues(pairs ...string) []CurrentValue {
	out := make([]CurrentValue, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, CurrentValue{Instance: pairs[i], Value: json.RawMessage(pairs[i+1]), Provenance: "observed"})
	}
	return out
}

// worstCaseValue is reconciliation's reduction (ADR-0456): drift when any
// instance disagrees with want, showing the newest disagreeing one, else the
// newest agreeing one.
func TestWorstCaseValue(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		want         string
		values       []CurrentValue
		instance     string
		drift, isNil bool
	}{
		{name: "nothing observed", want: `"a"`, isNil: true},
		{name: "one agreeing", want: `"a"`, values: instanceValues("ip", `"a"`), instance: "ip"},
		{name: "one disagreeing", want: `"a"`, values: instanceValues("ip", `"b"`), instance: "ip", drift: true},
		{name: "newest agrees, an older instance disagrees", want: `"a"`, values: instanceValues("ip", `"a"`, "serial", `"b"`), instance: "serial", drift: true},
		{name: "two disagree, the newest shown", want: `"a"`, values: instanceValues("serial", `"c"`, "ip", `"b"`), instance: "serial", drift: true},
		{name: "every instance agrees, the newest shown", want: `"a"`, values: instanceValues("serial", `"a"`, "ip", `"a"`), instance: "serial"},
		{name: "no want, the newest and no drift", values: instanceValues("serial", `"c"`, "ip", `"b"`), instance: "serial"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var want json.RawMessage
			if tc.want != "" {
				want = json.RawMessage(tc.want)
			}
			got, drift := worstCaseValue(want, tc.values)
			if tc.isNil {
				if got != nil || drift {
					t.Fatalf("got %+v drift %v, want nothing and no drift", got, drift)
				}
				return
			}
			if got == nil || got.Instance != tc.instance || drift != tc.drift {
				t.Fatalf("got %+v drift %v, want instance %q drift %v", got, drift, tc.instance, tc.drift)
			}
		})
	}
}

// agreeingValue is settlement's reduction (ADR-0456): the newest instance
// agreeing with the intent, else the newest observed, else nothing.
func TestAgreeingValue(t *testing.T) {
	t.Parallel()
	intended := &CurrentValue{Value: json.RawMessage(`"hdmi3"`), Provenance: "intended"}
	for _, tc := range []struct {
		name     string
		intended *CurrentValue
		values   []CurrentValue
		instance string
		isNil    bool
	}{
		{name: "nothing observed", intended: intended, isNil: true},
		{name: "an older instance agrees", intended: intended, values: instanceValues("ip", `"hdmi1"`, "serial", `"hdmi3"`), instance: "serial"},
		{name: "two agree, the newest", intended: intended, values: instanceValues("serial", `"hdmi3"`, "ip", `"hdmi3"`), instance: "serial"},
		{name: "none agrees, the newest", intended: intended, values: instanceValues("ip", `"hdmi1"`, "serial", `"hdmi2"`), instance: "ip"},
		{name: "no intent, the newest", values: instanceValues("ip", `"hdmi1"`), instance: "ip"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := agreeingValue(tc.intended, tc.values)
			if tc.isNil {
				if got != nil {
					t.Fatalf("got %+v, want nothing", got)
				}
				return
			}
			if got == nil || got.Instance != tc.instance {
				t.Fatalf("got %+v, want instance %q", got, tc.instance)
			}
		})
	}
}

// sameValue is the one comparison between an observed and a declared or
// intended value (#1202): equal JSON by meaning, not by bytes.
func TestSameValue(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, a, b string
		same       bool
	}{
		{"identical string", `"a"`, `"a"`, true},
		{"different string", `"a"`, `"b"`, false},
		{"bool", `true`, `true`, true},
		{"different bool", `true`, `false`, false},
		{"integer", `3`, `3`, true},
		{"different integer", `3`, `4`, false},
		{"number spelling", `3.0`, `3`, true},
		{"object key order and spacing", `{"b": 1, "a": [1, 2]}`, `{"a":[1,2],"b":1}`, true},
		{"different object", `{"a": 1}`, `{"a": 2}`, false},
		{"a bool is not its text", `true`, `"true"`, false},
		{"a number is not its text", `3`, `"3"`, false},
		{"not JSON on either side", `abc`, `abc`, true},
		{"not JSON against JSON", `abc`, `3`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameValue(json.RawMessage(tc.a), json.RawMessage(tc.b)); got != tc.same {
				t.Fatalf("sameValue(%s, %s) = %v, want %v", tc.a, tc.b, got, tc.same)
			}
		})
	}
}

// Settle and the two reductions agree on the same typed value.
func TestSettleAndReductionsCompareByMeaning(t *testing.T) {
	t.Parallel()
	now := time.Now()
	intended := &CurrentValue{Value: json.RawMessage(`3`), TS: now.Add(-time.Second)}
	if v := Settle(intended, &CurrentValue{Value: json.RawMessage(`3.0`), TS: now}, 60, now); v != SettlementSettled {
		t.Fatalf("an observation spelling the intended value another way: %q, want settled", v)
	}
	if v := Settle(intended, &CurrentValue{Value: json.RawMessage(`4`), TS: now}, 0, now); v != SettlementFailed {
		t.Fatalf("a different observation: %q, want failed", v)
	}
	if _, drift := worstCaseValue(json.RawMessage(`true`), instanceValues("ip", `true`)); drift {
		t.Fatal("worstCaseValue drifted on the same bool")
	}
	if got := agreeingValue(&CurrentValue{Value: json.RawMessage(`true`)}, instanceValues("a", `false`, "b", `true`)); got == nil || got.Instance != "b" {
		t.Fatalf("agreeingValue = %+v, want instance b", got)
	}
}
