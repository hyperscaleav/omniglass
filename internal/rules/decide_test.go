package rules

import "testing"

// A sample meeting fire opens; one meeting clear (or, absent a clear
// criterion, the negation of fire) clears; one meeting neither holds.
func TestDecide(t *testing.T) {
	fire := Criteria{Op: "gt", Value: []byte("30")}
	clear := Criteria{Op: "lt", Value: []byte("28")}
	for _, tc := range []struct {
		clear *Criteria
		value float64
		want  Action
	}{
		{&clear, 35, Fire},
		{&clear, 29, Hold},
		{&clear, 27, Clear},
		{nil, 35, Fire},
		{nil, 29, Clear},
	} {
		got, err := Decide(fire, tc.clear, tc.value)
		if err != nil || got != tc.want {
			t.Errorf("clear %v value %v = %v (%v), want %v", tc.clear, tc.value, got, err, tc.want)
		}
	}
	if _, err := Decide(fire, nil, "hot"); err == nil {
		t.Error("a text sample on a numeric rule decided without error")
	}
}
