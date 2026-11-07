package compressbench

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestSQLState(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, "accepted"},
		{&pgconn.PgError{Code: "55000", Message: "decompress it first"}, "refused, SQLSTATE 55000: decompress it first"},
		{errors.New("conn reset"), "error: conn reset"},
	} {
		if got := sqlState(tc.err); got != tc.want {
			t.Errorf("sqlState(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

// TestDisposition holds the probe's reading of the consumer to the storage
// classifier it names: a check violation is refused and Termed at once, and
// an object-state error is retried until the batch is lost.
func TestDisposition(t *testing.T) {
	if got := disposition(nil); !strings.Contains(got, "acked") {
		t.Errorf("nil: %q", got)
	}
	if got := disposition(&pgconn.PgError{Code: "23514"}); !strings.Contains(got, "IsRefusedWrite is true") {
		t.Errorf("23514: %q", got)
	}
	if got := disposition(&pgconn.PgError{Code: "55000"}); !strings.Contains(got, "IsRefusedWrite is false") {
		t.Errorf("55000: %q", got)
	}
}

func TestProbeTable(t *testing.T) {
	out := ProbeTable([]Probe{{Question: "Q2", Candidate: "pg_deltax", Step: "insert a | late row", Outcome: "refused\nretried"}})
	want := "| Q2 | pg_deltax | insert a \\| late row | refused retried |\n"
	if !strings.Contains(out, want) {
		t.Errorf("table lacks %q:\n%s", want, out)
	}
}
