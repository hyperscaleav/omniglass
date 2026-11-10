package labfootprint

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// The delivery read (#1641): every telemetry kind's loss across the ingest
// hop over the measured window, reconciled from the ledger each batch's
// receipt carries. A healthy lab reads exactly 0 for every kind. Each lane
// reads apart (#1696): the node lane's batches carry their node's boot, the
// push lane's (the API's push route and webhooks) the server's own, with no
// node.

// MaxDeliveryWindow is the longest window the read reconciles: receipts are
// pruned at a day, and a window near that would read pruned batches as
// missing.
const MaxDeliveryWindow = 23 * time.Hour

// missingLabel is what a missing item is on the node lane, at this stage:
// the ingest cannot yet tell these apart (see the lab guide).
const missingLabel = "never landed: Termed at ingest, dropped at the node, or lost in between"

// pushMissingLabel is what a missing item is on the push lane: the server
// mints the ledger as it publishes, and a publish the stream refused is
// taken back off it (the pusher was told and still holds its items), so a
// hole is a batch the stream stored and the ingest Termed, or one the stream
// gave up before it was read.
const pushMissingLabel = "never landed: Termed at ingest, or dropped by the stream before it was read"

// DeliveryLane is which way a batch came in: from a node, or pushed to the
// API.
type DeliveryLane string

// The lanes, in the order the table reads them.
const (
	LaneNode DeliveryLane = "node"
	LanePush DeliveryLane = "push"
)

// laneOf is the lane a receipt came in on: a receipt with no node is the
// push lane's.
func laneOf(node string) DeliveryLane {
	if node == "" {
		return LanePush
	}
	return LaneNode
}

// DeliveryRead is what the run read for the delivery table: the window, the
// receipts of every boot with a batch minted in it, and whether the
// telemetry stream had emptied when the settle ended.
type DeliveryRead struct {
	// Read is false when the run did not read receipts; Reason says why.
	Read     bool
	Reason   string
	Window   delivery.Window
	Receipts []delivery.Receipt
	// StreamClear is whether the telemetry consumer held no pending and no
	// unacked batch when the settle ended; Backlog is what it last read.
	StreamClear bool
	Backlog     Backlog
}

// DeliveryRow is one kind across one hop of one lane: the same keys for
// every kind, hop and lane, every outcome counted, and loss absent when
// nothing went in.
type DeliveryRow struct {
	Lane         DeliveryLane                `json:"lane"`
	Hop          delivery.Hop                `json:"hop"`
	Kind         delivery.Kind               `json:"kind"`
	In           uint64                      `json:"in"`
	Out          uint64                      `json:"out"`
	Loss         *float64                    `json:"loss,omitempty"`
	Deduplicated uint64                      `json:"deduplicated"`
	Outcomes     map[delivery.Outcome]uint64 `json:"outcomes"`
	// Cause is the largest outcome behind the loss, empty when nothing was
	// lost. The JSON reader has the outcomes themselves.
	Cause delivery.Outcome `json:"-"`
}

// UnsettledBoot is a boot whose batches were not all known when the settle
// ended: no ratio is read from it. A push-lane boot has no node.
type UnsettledBoot struct {
	Node string `json:"node"`
	Boot string `json:"boot"`
}

// DeliveryOf reconciles the read into one row per lane and kind the
// window's batches carried across the ingest hop, the node lane first, each
// in kind order, over the settled boots only. A boot still unsettled is
// named instead of read; while the stream still held batches, every boot is.
func DeliveryOf(d DeliveryRead) ([]DeliveryRow, []UnsettledBoot) {
	unsettled := delivery.Unsettled(d.Window, d.Receipts)
	if !d.StreamClear {
		unsettled = inWindowBoots(d.Window, d.Receipts)
	}
	skip := map[delivery.Boot]bool{}
	var named []UnsettledBoot
	for _, b := range unsettled {
		skip[b] = true
		named = append(named, UnsettledBoot{Node: b.Node, Boot: b.Boot})
	}
	settled := slices.DeleteFunc(slices.Clone(d.Receipts), func(r delivery.Receipt) bool {
		return skip[delivery.Boot{Node: r.Node, Boot: r.Boot}]
	})
	var rows []DeliveryRow
	for _, lane := range []DeliveryLane{LaneNode, LanePush} {
		mine := slices.DeleteFunc(slices.Clone(settled), func(r delivery.Receipt) bool { return laneOf(r.Node) != lane })
		rows = append(rows, laneRows(lane, delivery.Reconcile(d.Window, mine))...)
	}
	return rows, named
}

// laneRows is one row per kind a lane's reconcile carried, in kind order.
func laneRows(lane DeliveryLane, totals map[delivery.Kind]delivery.Total) []DeliveryRow {
	var rows []DeliveryRow
	for _, k := range delivery.Kinds {
		t, ok := totals[k]
		if !ok {
			continue
		}
		row := DeliveryRow{Lane: lane, Hop: delivery.Ingest, Kind: k, In: t.In, Out: t.Out, Deduplicated: t.Deduplicated,
			Outcomes: map[delivery.Outcome]uint64{}, Cause: t.Cause()}
		for _, o := range delivery.Outcomes {
			row.Outcomes[o] = t.Outcomes[o]
		}
		if l, ok := t.Loss(); ok {
			row.Loss = &l
		}
		rows = append(rows, row)
	}
	return rows
}

// inWindowBoots is every boot with a batch minted in the window, in node and
// boot order.
func inWindowBoots(w delivery.Window, receipts []delivery.Receipt) []delivery.Boot {
	seen := map[delivery.Boot]bool{}
	for _, r := range receipts {
		if r.Boot != "" && r.Seq > 0 && w.Contains(r.Minted) {
			seen[delivery.Boot{Node: r.Node, Boot: r.Boot}] = true
		}
	}
	return slices.SortedFunc(maps.Keys(seen), func(a, b delivery.Boot) int {
		return strings.Compare(a.Node+"\x00"+a.Boot, b.Node+"\x00"+b.Boot)
	})
}

// EndToEndOf is each kind's loss over every hop the node lane carries it
// across, 1 - prod(1 - loss), for the kinds with a ratio on every row. The
// push lane crosses the ingest hop alone, so its row is its end to end.
func EndToEndOf(rows []DeliveryRow) map[delivery.Kind]float64 {
	hops := map[delivery.Kind][]float64{}
	absent := map[delivery.Kind]bool{}
	for _, r := range rows {
		if r.Lane != LaneNode {
			continue
		}
		if r.Loss == nil {
			absent[r.Kind] = true
			continue
		}
		hops[r.Kind] = append(hops[r.Kind], *r.Loss)
	}
	out := map[delivery.Kind]float64{}
	for k, ls := range hops {
		if !absent[k] {
			out[k] = delivery.EndToEnd(ls...)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ReceiptsQuery writes one JSON object a line: every ledgered receipt of
// each boot that minted a batch in (from, to], from that boot's first batch
// in the window on (its batches after the window say whether it has
// settled), and the lower neighbor of that first batch, the boot's highest
// seq below it, which owns the gap below it however long before the window
// it was minted.
func ReceiptsQuery(from, to time.Time) string {
	return fmt.Sprintf(`select row_to_json(r) from (
		with first_in as (
			select boot, min(seq) as seq from telemetry_receipt
			where ledger is not null and minted > to_timestamp(%[1]s) and minted <= to_timestamp(%[2]s)
			group by boot)
		select t.node, t.boot, t.seq, t.minted, t.resolved, t.deliveries, t.ledger, t.outcome
		from telemetry_receipt t join first_in f on f.boot = t.boot
		where t.ledger is not null and (t.seq >= f.seq or t.seq = (
			select max(l.seq) from telemetry_receipt l
			where l.boot = f.boot and l.ledger is not null and l.seq < f.seq))
		order by t.node, t.boot, t.seq) r`,
		epoch(from), epoch(to))
}

func epoch(t time.Time) string { return strconv.FormatFloat(float64(t.UnixMicro())/1e6, 'f', 6, 64) }

// ParseReceipts reads what ReceiptsQuery writes.
func ParseReceipts(out string) ([]delivery.Receipt, error) {
	var rs []delivery.Receipt
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var row struct {
			Node       *string           `json:"node"`
			Boot       string            `json:"boot"`
			Seq        uint64            `json:"seq"`
			Minted     time.Time         `json:"minted"`
			Resolved   uint64            `json:"resolved"`
			Deliveries int               `json:"deliveries"`
			Ledger     delivery.Declared `json:"ledger"`
			Outcome    delivery.Tally    `json:"outcome"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("read a receipt %q: %w", line, err)
		}
		r := delivery.Receipt{Boot: row.Boot, Seq: row.Seq, Minted: row.Minted.UTC(), Resolved: row.Resolved,
			Deliveries: row.Deliveries, Declared: row.Ledger, Outcome: row.Outcome}
		if row.Node != nil {
			r.Node = *row.Node
		}
		rs = append(rs, r)
	}
	return rs, nil
}

// Backlog is the telemetry consumer's backlog as the server last gauged it:
// batches stored and not yet delivered, and delivered and not yet acked.
type Backlog struct {
	Pending, AckPending         float64
	SeenPending, SeenAckPending bool
}

// Clear is whether both gauges were read and both are 0: a gauge not yet
// published is unknown, never an empty stream.
func (b Backlog) Clear() bool {
	return b.SeenPending && b.SeenAckPending && b.Pending == 0 && b.AckPending == 0
}

// BacklogQuery is the latest of each consumer backlog gauge the server
// published after a time, one `name|value` a line.
func BacklogQuery(after time.Time) string {
	return fmt.Sprintf(`select distinct on (mt.name) mt.name, m.value from metric m
		join metric_series s on s.id = m.series_id join metric_type mt on mt.id = s.metric_type_id
		where mt.name in ('%s', '%s') and m.ts > to_timestamp(%s)
		order by mt.name, m.ts desc`,
		writerclass.BusConsumerPending, writerclass.BusConsumerAckPending, epoch(after))
}

// ParseBacklog reads what BacklogQuery writes.
func ParseBacklog(out string) Backlog {
	var b Backlog
	for _, line := range strings.Split(out, "\n") {
		name, value, ok := strings.Cut(strings.TrimSpace(line), "|")
		if !ok {
			continue
		}
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			continue
		}
		switch name {
		case writerclass.BusConsumerPending:
			b.Pending, b.SeenPending = v, true
		case writerclass.BusConsumerAckPending:
			b.AckPending, b.SeenAckPending = v, true
		}
	}
	return b
}

// deliveryTable is the report's delivery section: a row per lane, kind and
// hop, the node lane's end-to-end line, then each unsettled boot by name
// with no ratio, then what missing means on each lane that read it.
func (r ScaleReport) deliveryTable(b *strings.Builder) {
	if r.DeliveryNotRead != "" {
		fmt.Fprintf(b, "%-12s%s\n", "delivery", "not measured: "+r.DeliveryNotRead)
		return
	}
	row := func(cells ...string) {
		fmt.Fprintf(b, "%-12s%-6s%-8s%-9s%-9s%-9s%-7s%s\n", cells[0], cells[1], cells[2], cells[3], cells[4], cells[5], cells[6], cells[7])
	}
	row("delivery", "lane", "hop", "in", "out", "loss", "dedup", "cause")
	missing := map[DeliveryLane]bool{}
	for _, d := range r.Delivery {
		loss, cause := "-", "-"
		if d.Loss != nil {
			loss = ratio(*d.Loss)
		}
		if d.Cause != "" {
			cause = string(d.Cause)
			missing[d.Lane] = missing[d.Lane] || d.Cause == delivery.Missing
		}
		row(string(d.Kind), string(d.Lane), string(d.Hop), strconv.FormatUint(d.In, 10), strconv.FormatUint(d.Out, 10), loss,
			strconv.FormatUint(d.Deduplicated, 10), cause)
	}
	if slices.ContainsFunc(r.Delivery, func(d DeliveryRow) bool { return d.Lane == LaneNode }) {
		cells := []string{fmt.Sprintf("%-12s%s", "end to end", LaneNode)}
		for _, k := range delivery.Kinds {
			v := "-"
			if l, ok := r.DeliveryEndToEnd[k]; ok {
				v = ratio(l)
			}
			cells = append(cells, string(k)+" "+v)
		}
		b.WriteString(strings.Join(cells, "  ") + "\n")
	}
	for _, u := range r.DeliveryUnsettled {
		who := "node " + u.Node
		if laneOf(u.Node) == LanePush {
			who = "push lane"
		}
		fmt.Fprintf(b, "%-12s%s, boot %s: no ratio\n", "unsettled", who, u.Boot)
	}
	for _, l := range []struct {
		lane  DeliveryLane
		label string
	}{{LaneNode, missingLabel}, {LanePush, pushMissingLabel}} {
		if missing[l.lane] {
			fmt.Fprintf(b, "%-12s%-6s%s\n", "missing", l.lane, l.label)
		}
	}
	if r.DeliveryNote != "" {
		b.WriteString(r.DeliveryNote + "\n")
	}
}

// ratio is a loss for the table: exact 0 as 0, otherwise four significant
// figures.
func ratio(v float64) string { return strconv.FormatFloat(v, 'g', 4, 64) }

// gauge is a backlog gauge for a note: its value, or unread.
func gauge(v float64, seen bool) string {
	if !seen {
		return "unread"
	}
	return strconv.FormatFloat(v, 'f', 0, 64)
}
