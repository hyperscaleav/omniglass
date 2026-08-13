package storage

import (
	"testing"
	"time"
)

// The whole point of tsSince is that no timestamp is ever computed in this
// process, so the assertions are about what it does NOT emit as much as what it
// does: a bound predicate names now() and binds seconds, and the unbounded case
// names nothing at all rather than a date far enough back to look harmless.
func TestTsSinceComputesTheBoundaryInTheDatabase(t *testing.T) {
	cases := []struct {
		name     string
		window   time.Duration
		args     []any
		wantSQL  string
		wantArgs []any
	}{
		{
			name:     "a window renders a bound against the database clock",
			window:   30 * 24 * time.Hour,
			args:     []any{"owner", "health"},
			wantSQL:  "ts >= now() - make_interval(secs => $3)",
			wantArgs: []any{"owner", "health", 2592000.0},
		},
		{
			name:     "the placeholder follows the caller's own arguments",
			window:   time.Minute,
			args:     []any{"comp"},
			wantSQL:  "ts >= now() - make_interval(secs => $2)",
			wantArgs: []any{"comp", 60.0},
		},
		{
			name:     "no arguments still numbers from one",
			window:   time.Second,
			wantSQL:  "ts >= now() - make_interval(secs => $1)",
			wantArgs: []any{1.0},
		},
		{
			name:     "a zero window is the whole series, and binds nothing",
			window:   0,
			args:     []any{"comp", 10},
			wantSQL:  "true",
			wantArgs: []any{"comp", 10},
		},
		{
			name:     "a negative window is unbounded too, not a bound in the future",
			window:   -time.Hour,
			args:     []any{"comp"},
			wantSQL:  "true",
			wantArgs: []any{"comp"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, args := tsSince("ts", tc.window, tc.args...)
			if sql != tc.wantSQL {
				t.Errorf("predicate = %q, want %q", sql, tc.wantSQL)
			}
			if len(args) != len(tc.wantArgs) {
				t.Fatalf("args = %v, want %v", args, tc.wantArgs)
			}
			for i := range args {
				if args[i] != tc.wantArgs[i] {
					t.Errorf("arg %d = %v (%T), want %v (%T)", i, args[i], args[i], tc.wantArgs[i], tc.wantArgs[i])
				}
			}
		})
	}
}
