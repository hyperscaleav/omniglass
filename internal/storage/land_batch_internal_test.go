package storage

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestIsRefusedWrite: the ingest drops a batch at once only for a write no
// retry changes, and redelivers anything else.
func TestIsRefusedWrite(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		want bool
	}{
		{"an owner gone", fmt.Errorf("storage: sample disp-1/uptime: %w", ErrComponentNotFound), true},
		{"a writer class refusing", fmt.Errorf("storage: property sample: %w", ErrKeyWriterRefused), true},
		{"an ambiguous owner", fmt.Errorf("storage: sample: %w", &ErrAmbiguousName{Kind: "component", Ref: "disp-1"}), true},
		{"a data exception", &pgconn.PgError{Code: "22021"}, true},
		{"a CHECK violation", &pgconn.PgError{Code: "23514"}, true},
		// A parent row deleted under the batch (a metric type, a calc rule):
		// the redelivery re-derives against the registry as it is now and
		// lands the rest, so these are not refusals.
		{"a foreign key gone under the batch", &pgconn.PgError{Code: "23503"}, false},
		{"a type lookup gone null under the batch", &pgconn.PgError{Code: "23502"}, false},
		{"a unique race", &pgconn.PgError{Code: "23505"}, false},
		{"a deadlock", &pgconn.PgError{Code: "40P01"}, false},
		{"a connection lost", &pgconn.PgError{Code: "08006"}, false},
		{"anything else", errors.New("the database blinked"), false},
	} {
		if got := IsRefusedWrite(c.err); got != c.want {
			t.Errorf("%s: IsRefusedWrite = %v, want %v", c.name, got, c.want)
		}
	}
}
