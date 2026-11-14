package api

import (
	"encoding/json"
	"testing"
)

func TestInputScalarUnmarshal(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		`{"a":"x"}`:     "x",
		`{"a":10161}`:   "10161",
		`{"a":"10161"}`: "10161",
		`{"a":1.5}`:     "1.5",
		`{"a":1e3}`:     "1000",
		`{"a":0}`:       "0",
	} {
		var got inputScalars
		if err := json.Unmarshal([]byte(in), &got); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if string(got["a"]) != want {
			t.Errorf("%s: got %q, want %q", in, got["a"], want)
		}
	}
	for _, in := range []string{`{"a":true}`, `{"a":{}}`, `{"a":[1]}`} {
		var got inputScalars
		if err := json.Unmarshal([]byte(in), &got); err == nil {
			t.Errorf("%s: want an error, got %v", in, got)
		}
	}
}

func TestInputScalarsStrings(t *testing.T) {
	t.Parallel()
	if (inputScalars)(nil).strings() != nil {
		t.Error("nil map should stay nil")
	}
	if got := (inputScalars{"port": "1"}).strings(); got["port"] != "1" || len(got) != 1 {
		t.Errorf("got %v", got)
	}
}
