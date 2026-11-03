package storage

import (
	"errors"
	"slices"
	"testing"
)

// pickInstance is the rule a bucketed read follows to stay one series (#1485):
// a named instance is read as asked, an unnamed read takes the one measured
// instance, and two measured instances are refused with both listed.
func TestPickInstance(t *testing.T) {
	named := "tls"
	empty := ""
	for _, tc := range []struct {
		name     string
		asked    *string
		measured []string
		want     string
		wantAmb  []string
	}{
		{name: "named is read as asked", asked: &named, measured: []string{"ssh", "tls"}, want: "tls"},
		{name: "the empty instance can be named", asked: &empty, measured: []string{"", "ssh"}, want: ""},
		{name: "one measured is the default", measured: []string{"ssh"}, want: "ssh"},
		{name: "none measured reads an empty grid", measured: nil, want: ""},
		{name: "two measured are refused", measured: []string{"ssh", "tls"}, wantAmb: []string{"ssh", "tls"}},
	} {
		got, err := pickInstance("k", tc.asked, tc.measured)
		if tc.wantAmb != nil {
			var amb *AmbiguousInstanceError
			if !errors.As(err, &amb) || !slices.Equal(amb.Instances, tc.wantAmb) {
				t.Errorf("%s: err = %v, want AmbiguousInstanceError %v", tc.name, err, tc.wantAmb)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%s: got %q %v, want %q", tc.name, got, err, tc.want)
		}
	}
}
