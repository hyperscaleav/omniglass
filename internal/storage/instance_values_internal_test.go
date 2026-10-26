package storage

import (
	"encoding/json"
	"testing"
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
