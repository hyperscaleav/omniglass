package storage

import (
	"strings"
	"testing"
)

// A bare icmp endpoint's count is read by the outpost into an int
// (endpointParams.Count), which takes a JSON integer literal and nothing else:
// a count spelled 3.0 or 1e1 is a whole number to a float64 but faults the
// outpost's parse on every probe, so the write refuses each spelling the
// outpost would, naming count, and accepts the ones it reads.
func TestABareICMPCountIsTheIntegerSpellingTheOutpostReads(t *testing.T) {
	t.Parallel()
	cases := []struct {
		params string
		refuse bool
	}{
		{`{"target":"10.0.0.1","count":3}`, false},
		{`{"target":"10.0.0.1","count":0}`, false},
		{`{"target":"10.0.0.1"}`, false},
		{`{"target":"10.0.0.1","count":3.0}`, true},
		{`{"target":"10.0.0.1","count":1e1}`, true},
		{`{"target":"10.0.0.1","count":1.5}`, true},
		{`{"target":"10.0.0.1","count":-1}`, true},
		{`{"target":"10.0.0.1","count":"three"}`, true},
	}
	for _, tc := range cases {
		err := checkBareParams("icmp", []byte(tc.params))
		if !tc.refuse {
			if err != nil {
				t.Errorf("%s = %v, want accepted", tc.params, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), "count") {
			t.Errorf("%s = %v, want refused naming count", tc.params, err)
		}
	}
}
