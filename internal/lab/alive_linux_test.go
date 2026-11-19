//go:build linux

package lab

import "testing"

// Field 22 of /proc/<pid>/stat is the start time, found after the last ")"
// so a command name with spaces or parentheses does not shift the fields.
func TestParseStatStart(t *testing.T) {
	for name, tc := range map[string]struct {
		stat string
		want string
		ok   bool
	}{
		"plain":                          {"42 (prog) S 1 42 42 0 -1 4194560 100 0 0 0 0 0 0 0 20 0 1 0 987654 1000 100 18446744073709551615 0", "987654", true},
		"a name with spaces and a paren": {"42 (my ) prog) S 1 42 42 0 -1 4194560 100 0 0 0 0 0 0 0 20 0 1 0 555 1000 100 0", "555", true},
		"truncated":                      {"42 (prog) S 1 42", "", false},
		"no name":                        {"42 prog S 1", "", false},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := parseStatStart(tc.stat)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("got %q, %v; want %q, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}
