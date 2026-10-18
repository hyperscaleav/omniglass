package rules

import (
	"encoding/json"
	"math"
	"testing"
)

func crit(t *testing.T, s string) Criteria {
	t.Helper()
	var c Criteria
	if err := json.Unmarshal([]byte(s), &c); err != nil {
		t.Fatalf("unmarshal %s: %v", s, err)
	}
	return c
}

// A property criterion compares the sample's text value: equal or not equal.
func TestPropertyCriteria(t *testing.T) {
	down := crit(t, `{"op":"eq","value":"down"}`)
	if err := down.Validate(LaneProperty); err != nil {
		t.Fatal(err)
	}
	if ok, _ := down.Eval("down"); !ok {
		t.Error("eq down did not match down")
	}
	if ok, _ := down.Eval("up"); ok {
		t.Error("eq down matched up")
	}
	if ok, _ := crit(t, `{"op":"ne","value":"up"}`).Eval("down"); !ok {
		t.Error("ne up did not match down")
	}
	if err := crit(t, `{"op":"gt","value":"down"}`).Validate(LaneProperty); err == nil {
		t.Error("an ordered comparison on a text property validated")
	}
}

// A metric criterion compares the sample's number, with every operator.
func TestMetricCriteria(t *testing.T) {
	for _, tc := range []struct {
		c     string
		value float64
		want  bool
	}{
		{`{"op":"gt","value":80}`, 81, true},
		{`{"op":"gt","value":80}`, 80, false},
		{`{"op":"ge","value":80}`, 80, true},
		{`{"op":"lt","value":0.5}`, 0.25, true},
		{`{"op":"le","value":0.5}`, 0.75, false},
		{`{"op":"eq","value":0}`, 0, true},
		{`{"op":"ne","value":0}`, 1, true},
	} {
		c := crit(t, tc.c)
		if err := c.Validate(LaneMetric); err != nil {
			t.Fatalf("%s: %v", tc.c, err)
		}
		if got, err := c.Eval(tc.value); err != nil || got != tc.want {
			t.Errorf("%s on %v = %v (%v), want %v", tc.c, tc.value, got, err, tc.want)
		}
	}
	if err := crit(t, `{"op":"gt","value":"high"}`).Validate(LaneMetric); err == nil {
		t.Error("a metric criterion with a text threshold validated")
	}
}

func TestCriteriaRefusesNonsense(t *testing.T) {
	for _, s := range []string{`{"op":"like","value":"x"}`, `{"op":"eq"}`, `{}`} {
		if err := crit(t, s).Validate(LaneProperty); err == nil {
			t.Errorf("%s validated", s)
		}
	}
	if _, err := crit(t, `{"op":"gt","value":1}`).Eval("not a number"); err == nil {
		t.Error("a numeric criterion evaluated a text sample")
	}
}

// The clear criterion defaults to the negation of the fire criterion.
func TestNegate(t *testing.T) {
	fire := crit(t, `{"op":"eq","value":"down"}`)
	clear := fire.Negate()
	if ok, _ := clear.Eval("up"); !ok {
		t.Error("the negation of eq down did not match up")
	}
	if ok, _ := clear.Eval("down"); ok {
		t.Error("the negation of eq down matched down")
	}
	gt := crit(t, `{"op":"gt","value":80}`).Negate()
	if ok, _ := gt.Eval(80.0); !ok {
		t.Error("the negation of gt 80 did not match 80")
	}
}

// A criterion judges a sample by its lane's type: a text criterion takes
// only text, and a number that is not a number (NaN, infinity) decides
// nothing rather than holding neither fire nor clear forever.
func TestCriteriaRefusesTheWrongType(t *testing.T) {
	if _, err := crit(t, `{"op":"eq","value":"1000000"}`).Eval(1000000.0); err == nil {
		t.Error("a text criterion compared a number by its printed form")
	}
	for _, v := range []any{"NaN", "Inf", math.NaN(), math.Inf(1)} {
		if _, err := crit(t, `{"op":"gt","value":5}`).Eval(v); err == nil {
			t.Errorf("a numeric criterion evaluated %v", v)
		}
	}
}
