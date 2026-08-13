package storage

import (
	"fmt"
	"time"
)

// tsSince renders the timestamp predicate a windowed history read filters on,
// together with the argument list to bind it against. The caller passes the
// arguments its own placeholders already use, and the window's argument, when
// there is one, lands after them.
//
// The boundary is computed by the DATABASE, `now()` minus the window, rather
// than in the server process (#719). Every `ts` on the telemetry tables is
// `timestamp with time zone default now()`, so a boundary built from
// `time.Now()` puts two clocks on one comparison and the edge of a history
// window moves with the skew between the host and the database: nothing an
// operator sees at a thirty-day window, and a difference between a local
// database and a remote one that surfaces as an unreproducible report.
//
// ADR-0108 settled the same defect on the settle paths by reading `select
// now()` inside the transaction, at the stated cost of one round trip. These
// are READ paths, and the difference that matters is that they need a BOUND
// rather than a value: `Settle` compares in Go and must hold `now`, while a
// history read only ever hands its boundary to a `where`. So the database's
// clock is available here for no round trip at all, which is why the window
// travels as a duration and the instant is never named.
//
// A window of zero or less is unbounded, the whole series, and renders no
// predicate rather than a boundary in the distant past. A predicate would have
// to name a timestamp, which is the thing this exists to stop a caller doing.
//
// `now()` is `transaction_timestamp`. These reads are single statements on the
// pool, so that is the statement's own start, from the same server clock that
// stamped every row it is comparing against.
func tsSince(col string, window time.Duration, args ...any) (string, []any) {
	if window <= 0 {
		return "true", args
	}
	return fmt.Sprintf("%s >= now() - make_interval(secs => $%d)", col, len(args)+1), append(args, window.Seconds())
}
