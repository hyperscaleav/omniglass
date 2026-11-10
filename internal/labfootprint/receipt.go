package labfootprint

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/closedloop"
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
// minus the margin, and of those, how many landed before t0, how many from
// t0 until the lane drained past the start read (the backlog of batches the
// bus acknowledged before it, stopped at t1), and how many after t1. A
// receipt's batch id is the subject the node published on and its message
// id, so the node's prefix is its subject and a space; starts_with keeps a
// node name's own characters literal.
func receiptCountQuery(nodeName string, t0, drained, t1 time.Time) string {
	prefix := collection.TelemetrySubject(nodeName) + " "
	return fmt.Sprintf(`select count(*),
		count(*) filter (where landed_at < to_timestamp(%[2]s)),
		count(*) filter (where landed_at >= to_timestamp(%[2]s) and landed_at < to_timestamp(%[3]s)
			and landed_at <= to_timestamp(%[4]s)),
		count(*) filter (where landed_at > to_timestamp(%[4]s))
		from telemetry_receipt
		where starts_with(batch_id, %[1]s) and landed_at >= to_timestamp(%[5]s)`,
		sqlLiteral(prefix), epochLiteral(t0), epochLiteral(drained), epochLiteral(t1), epochLiteral(t0.Add(-receiptMargin)))
}

// parseReceiptCount reads the query's one row as psql -At prints it.
func parseReceiptCount(out string) (closedloop.ReceiptCount, error) {
	f := strings.Split(strings.TrimSpace(out), "|")
	if len(f) != 4 {
		return closedloop.ReceiptCount{}, fmt.Errorf("receipt count: want 4 columns, got %q", strings.TrimSpace(out))
	}
	var n [4]int64
	for i, s := range f {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return closedloop.ReceiptCount{}, fmt.Errorf("receipt count: column %d: %w", i+1, err)
		}
		n[i] = v
	}
	return closedloop.ReceiptCount{Landed: n[0], Margin: n[1], Backlog: n[2], AfterEnd: n[3]}, nil
}

// drainedQuery reads whether the ingest lane has drained past at, and the
// database's clock. The lane lands one batch at a time in the order the
// stream stored them, a redelivery included (MaxAckPending 1 in
// internal/bus), so once it has landed a sample a node probed after at,
// every batch the bus acknowledged before at has landed or been refused,
// and every receipt of one landed before the clock this reads. Only a
// node's own samples count: a push or a webhook count the server writes
// outside the lane says nothing of it, and a sample stamped past the
// database's clock is a node's clock running ahead.
func drainedQuery(at time.Time) string {
	return fmt.Sprintf(`select case when exists (
			select 1 from metric m join metric_series s on s.id = m.series_id
			where m.source_rule is null and s.collector_node_id is not null
				and m.ts > to_timestamp(%s) and m.ts <= clock_timestamp())
		then 't' else 'f' end,
		extract(epoch from clock_timestamp())::text`, epochLiteral(at))
}

// parseDrained reads the drain query's one row as psql -At prints it.
func parseDrained(out string) (bool, time.Time, error) {
	flag, clock, ok := strings.Cut(strings.TrimSpace(out), "|")
	if !ok || (flag != "t" && flag != "f") {
		return false, time.Time{}, fmt.Errorf("drain: want t or f and the clock, got %q", strings.TrimSpace(out))
	}
	at, err := parseEpoch(clock)
	if err != nil {
		return false, time.Time{}, err
	}
	return flag == "t", at, nil
}

// sqlLiteral quotes s as a standard SQL string literal.
func sqlLiteral(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// epochLiteral is t as seconds since the epoch, to the microsecond.
func epochLiteral(t time.Time) string {
	return fmt.Sprintf("%d.%06d", t.Unix(), t.Nanosecond()/1000)
}
