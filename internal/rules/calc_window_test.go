package rules

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func windowed(alias, key, valueType string, seconds int) TypedInput {
	return TypedInput{Alias: alias, Key: key, ValueType: valueType, Window: seconds}
}

// A windowed input binds as the list of its key's samples in the window,
// each a record with value, at (unix seconds: an expression environment holds
// no time.Time) and instance (#1142), and the list functions
// fold it by value, so avg(rtt) over a window is the mean of its samples.
func TestCompileCalcWindowedInput(t *testing.T) {
	c, err := CompileCalc(OwnerComponent, []TypedInput{windowed("rtt", "icmp-rtt", ValueNumber, 300)},
		CalcReduce{Expr: "avg(rtt)"}, ValueNumber)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, b := range c.Env.Bindings() {
		got[b.Name] = b.Type
	}
	want := map[string]string{"rtt": "[]record", "rtt[].value": "float64", "rtt[].at": "float64", "rtt[].instance": "string"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bindings %v, want %v", got, want)
	}
	at := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	samples := []any{CalcSample{Value: 10.0, At: at}, CalcSample{Value: 20.0, At: at.Add(time.Second)}, CalcSample{Value: 60.0, At: at.Add(2 * time.Second)}}
	v, err := c.Reduce([][]any{samples})
	if err != nil {
		t.Fatal(err)
	}
	if v != 30.0 {
		t.Errorf("avg over the window = %v, want 30", v)
	}
	for expr, want := range map[string]any{
		"max(rtt)":                          60.0,
		"min(rtt)":                          10.0,
		"sum(rtt)":                          90.0,
		"len(rtt) * 1.0":                    3.0,
		"sum(rtt, .value > 15 ? 1.0 : 0.0)": 2.0,
		"rtt[len(rtt)-1].value":             60.0,
		"rtt[2].at - rtt[0].at":             2.0,
	} {
		c := mustCompile(t, OwnerComponent, []TypedInput{windowed("rtt", "icmp-rtt", ValueNumber, 300)}, CalcReduce{Expr: expr}, ValueNumber)
		got, err := c.Reduce([][]any{samples})
		if err != nil {
			t.Errorf("%s: %v", expr, err)
			continue
		}
		if got != want {
			t.Errorf("%s = %v, want %v", expr, got, want)
		}
	}
	b := mustCompile(t, OwnerComponent, []TypedInput{windowed("up", "reachable", ValueBool, 60)}, CalcReduce{Expr: "all(up)"}, ValueBool)
	if got, err := b.Reduce([][]any{{CalcSample{Value: true, At: at}, CalcSample{Value: false, At: at}}}); err != nil || got != false {
		t.Errorf("all over a window of bools = %v, %v; want false", got, err)
	}
	// A window and a current-value input side by side.
	m := mustCompile(t, OwnerComponent, []TypedInput{windowed("rtt", "icmp-rtt", ValueNumber, 300), in("cap", "rtt-cap", ValueNumber)},
		CalcReduce{Expr: "avg(rtt) / max(cap)"}, ValueNumber)
	if got, err := m.Reduce([][]any{samples, {60.0}}); err != nil || got != 0.5 {
		t.Errorf("window over current = %v, %v; want 0.5", got, err)
	}
	// An empty window is a missing input, as a key with no value is.
	if _, err := c.Reduce([][]any{{}}); !errors.Is(err, ErrMissingInput) {
		t.Errorf("an empty window: %v, want ErrMissingInput", err)
	}
	// A sample of the wrong type is refused.
	if _, err := c.Reduce([][]any{{CalcSample{Value: "x", At: at}}}); err == nil {
		t.Error("a text sample on a number window reduced")
	}
}

// A window is 1s to MaxCalcWindow, on a component rule only (#1142): a
// system rule's windows are #1143's.
func TestCompileCalcWindowRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		owner  string
		window int
	}{
		"negative":      {OwnerComponent, -1},
		"over the cap":  {OwnerComponent, int(MaxCalcWindow/time.Second) + 1},
		"a system rule": {OwnerSystem, 60},
	} {
		_, err := CompileCalc(tc.owner, []TypedInput{windowed("rtt", "icmp-rtt", ValueNumber, tc.window)}, CalcReduce{Expr: "avg(rtt)"}, ValueNumber)
		var ce *CalcError
		if !errors.As(err, &ce) || ce.Field != "inputs[0].window_seconds" {
			t.Errorf("%s: %v, want a refusal on inputs[0].window_seconds", name, err)
		}
	}
	if _, err := CompileCalc(OwnerComponent, []TypedInput{windowed("rtt", "icmp-rtt", ValueNumber, int(MaxCalcWindow/time.Second))},
		CalcReduce{Expr: "avg(rtt)"}, ValueNumber); err != nil {
		t.Errorf("a window at the cap: %v", err)
	}
}
