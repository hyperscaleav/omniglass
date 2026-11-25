package storage

import "testing"

// A rule's alarm names the instance its rule judged (one alarm per endpoint
// for reachability-lost), under either rule segment the key may carry; a
// hand-raised alarm and a rule over one series name none.
func TestAlarmInstance(t *testing.T) {
	for _, tc := range []struct{ key, want string }{
		{"rule:01a2133c-0270-7888-aad8-4dd37246c331:snmp", "snmp"},
		{"rule:01a2133c-0270-7888-aad8-4dd37246c331:http", "http"},
		{"rule:reachability-lost:web-admin", "web-admin"},
		{"rule:01a2133c-0270-7888-aad8-4dd37246c331:port:2", "port:2"},
		{"rule:01a2133c-0270-7888-aad8-4dd37246c331", ""},
		{"Bezel cracked", ""},
		{"", ""},
	} {
		if got := (Alarm{DedupKey: tc.key}).Instance(); got != tc.want {
			t.Errorf("Instance() of %q = %q, want %q", tc.key, got, tc.want)
		}
	}
}
