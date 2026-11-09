package labfootprint

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// receiptMargin is how far before the window's start a node's receipts are
// counted: the stream's ack wait (telemetryAckWait in internal/bus), the
// longest a batch the bus acknowledged after the start read can have landed
// before it.
const receiptMargin = 30 * time.Second

// maxEndpointWindow is the longest window the receipt count can read: the
// server prunes receipts past a 24 h horizon (receiptHorizon in internal/bus),
// and a window that long would lose its earliest receipts to the prune.
const maxEndpointWindow = 23 * time.Hour

// receiptCountQuery counts one node's telemetry receipts around the window
// [t0, t1], each read off the database's clock: every receipt landed from t0
// minus the margin, and of those, how many landed before t0 and after t1. A
// receipt's batch id is the subject the node published on and its message
// id, so the node's prefix is its subject and a space; starts_with keeps a
// node name's own characters literal.
func receiptCountQuery(nodeName string, t0, t1 time.Time) string {
	prefix := collection.TelemetrySubject(nodeName) + " "
	return fmt.Sprintf(`select count(*),
		count(*) filter (where landed_at < to_timestamp(%[2]s)),
		count(*) filter (where landed_at > to_timestamp(%[3]s))
		from telemetry_receipt
		where starts_with(batch_id, %[1]s) and landed_at >= to_timestamp(%[4]s)`,
		sqlLiteral(prefix), epochLiteral(t0), epochLiteral(t1), epochLiteral(t0.Add(-receiptMargin)))
}

// parseReceiptCount reads the query's one row as psql -At prints it.
func parseReceiptCount(out string) (ReceiptCount, error) {
	f := strings.Split(strings.TrimSpace(out), "|")
	if len(f) != 3 {
		return ReceiptCount{}, fmt.Errorf("receipt count: want 3 columns, got %q", strings.TrimSpace(out))
	}
	var n [3]int64
	for i, s := range f {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return ReceiptCount{}, fmt.Errorf("receipt count: column %d: %w", i+1, err)
		}
		n[i] = v
	}
	return ReceiptCount{Landed: n[0], Margin: n[1], AfterEnd: n[2]}, nil
}

// sqlLiteral quotes s as a standard SQL string literal.
func sqlLiteral(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// epochLiteral is t as seconds since the epoch, to the microsecond.
func epochLiteral(t time.Time) string {
	return fmt.Sprintf("%d.%06d", t.Unix(), t.Nanosecond()/1000)
}
