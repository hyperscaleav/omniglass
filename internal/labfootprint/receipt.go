package labfootprint

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/closedloop"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/node"
)

// maxEndpointWindow is the longest window the receipt count can read: the
// server prunes receipts past a 24 h horizon (ReceiptHorizon in internal/bus),
// and a window that long would lose its earliest receipts to the prune.
const maxEndpointWindow = 23 * time.Hour

// receiptCountQuery counts one node's telemetry receipts around the window
// [t0, t1], each read off the database's clock: every receipt landed from t0
// minus the margin, and of those, how many landed before t0, how many from
// t0 until the lanes drained past the start read (the backlog of batches the
// bus acknowledged before it, stopped at t1), and how many after t1. A
// receipt's batch id is the subject the node published on and its message
// id, so the node's prefix is its subject and a space; starts_with keeps a
// node name's own characters literal. A Term receipt (#1691) landed
// nothing, so it never counts.
func receiptCountQuery(nodeName string, t0, drained, t1 time.Time) string {
	prefix := collection.TelemetrySubject(nodeName) + " "
	return fmt.Sprintf(`select count(*),
		count(*) filter (where landed_at < to_timestamp(%[2]s)),
		count(*) filter (where landed_at >= to_timestamp(%[2]s) and landed_at < to_timestamp(%[3]s)
			and landed_at <= to_timestamp(%[4]s)),
		count(*) filter (where landed_at > to_timestamp(%[4]s))
		from telemetry_receipt
		where starts_with(batch_id, %[1]s) and landed_at >= to_timestamp(%[5]s) and not termed`,
		sqlLiteral(prefix), epochLiteral(t0), epochLiteral(drained), epochLiteral(t1), epochLiteral(t0.Add(-closedloop.ReceiptMargin)))
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

// liveWindow is how long before a drain's time a node's last heartbeat still
// counts it live: three of the default 30 s heartbeats, so a node whose next
// heartbeat is not due yet is waited for, while a node silent for longer is
// not.
const liveWindow = 3 * node.DefaultHeartbeat

// drainedQuery reads whether the ingest lanes have drained past at, and the
// database's clock. Each lane lands its batches one at a time in the order
// the stream stored them, a redelivery included (MaxAckPending 1 in
// internal/bus), and a node's batches all share one lane (ADR-1896), so once
// a node's sample probed after at has landed, every batch of that node's
// lane the bus acknowledged before at has landed or been refused. One node's
// sample says nothing of another lane, so the lanes have drained once every
// live node that collects has landed one: live is a heartbeat within live
// before at, read off at rather than the clock so the set holds still while
// the drain is polled, and a heartbeat reaches the server outside the lanes,
// so a backlog does not hide a node, while a node silent for longer is not
// waited for. A live node that collects but lands nothing holds the drain
// until its bound, which fails the measure rather than reading it short.
// Only a node's own samples count: a push or a webhook count the server
// writes outside the lanes says nothing of them, and a sample stamped past
// the database's clock is a node's clock running ahead. No live node that
// collects reads as not drained.
func drainedQuery(at time.Time, live time.Duration) string {
	return fmt.Sprintf(`with live as (
			select n.principal_id from node n
			where n.last_heartbeat_at > to_timestamp(%[2]s)
				and exists (select 1 from metric_series s where s.collector_node_id = n.principal_id))
		select case when exists (select 1 from live) and not exists (
			select 1 from live l where not exists (
				select 1 from metric_series s
				where s.collector_node_id = l.principal_id and exists (
					select 1 from metric m
					where m.series_id = s.id and m.source_rule is null
						and m.ts > to_timestamp(%[1]s) and m.ts <= clock_timestamp())))
		then 't' else 'f' end,
		extract(epoch from clock_timestamp())::text`, epochLiteral(at), epochLiteral(at.Add(-live)))
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
