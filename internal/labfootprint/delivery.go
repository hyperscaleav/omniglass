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

// The delivery read (#1641): every telemetry kind's loss across the node hop
// (#1692) and the ingest hop over the measured window, reconciled from the
// ledger each batch's receipt carries. A healthy lab reads exactly 0 for
// every kind on every hop.

// MaxDeliveryWindow is the longest window the read reconciles: receipts are
// pruned at a day, and a window near that would read pruned batches as
// missing.
const MaxDeliveryWindow = 23 * time.Hour

// missingLabel is what a missing item is: a batch the server Terms whole
// reads under its own cause (#1691) and what the node dropped is the node
// row's, under its cause, so what is left is an item the node shipped that
// never decoded at ingest or was lost between the stream and the store,
// which the read cannot yet tell apart (see the lab guide).
const missingLabel = "shipped by the node, never landed: undecodable at ingest, or lost between the stream and the store"

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

// DeliveryRow is one kind across one hop: the same keys for every kind and
// hop, every outcome counted, and loss absent when nothing went in.
type DeliveryRow struct {
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

// UnsettledBoot is a node boot whose batches were not all known when the
// settle ended: no ratio is read from it.
type UnsettledBoot struct {
	Node string `json:"node"`
	Boot string `json:"boot"`
}

// EndedBoot is a boot that cannot settle because its node started again
// (#1692): it is read up to Last, its highest seq that landed, and whatever
// it minted after that went with the process, never shipped and uncounted
// (#1733 counts it). Next is the boot that followed and NextMinted its first
// landed batch.
type EndedBoot struct {
	Node       string    `json:"node"`
	Boot       string    `json:"boot"`
	Last       uint64    `json:"last_seq"`
	Next       string    `json:"next_boot"`
	NextMinted time.Time `json:"next_minted"`
}

// DeliveryOf reconciles the read into one row per kind the window's batches
// carried across each hop, the node's then the ingest's, in kind order, over
// the settled boots only. A boot still unsettled is named instead of read;
// while the stream still held batches, every boot is. With the stream clear,
// an unsettled boot its node followed with another boot is ended: it will
// never settle, so it is read as far as it landed and named as ended.
func DeliveryOf(d DeliveryRead) ([]DeliveryRow, []UnsettledBoot, []EndedBoot) {
	unsettled := delivery.Unsettled(d.Window, d.Receipts)
	var ended map[delivery.Boot]delivery.Ended
	if !d.StreamClear {
		unsettled = inWindowBoots(d.Window, d.Receipts)
	} else {
		ended = delivery.EndedOf(unsettled, d.Receipts)
	}
	skip := map[delivery.Boot]bool{}
	var named []UnsettledBoot
	var gone []EndedBoot
	for _, b := range unsettled {
		if e, ok := ended[b]; ok {
			gone = append(gone, EndedBoot{Node: b.Node, Boot: b.Boot, Last: e.Last, Next: e.Next, NextMinted: e.NextMinted})
			continue
		}
		skip[b] = true
		named = append(named, UnsettledBoot{Node: b.Node, Boot: b.Boot})
	}
	settled := slices.DeleteFunc(slices.Clone(d.Receipts), func(r delivery.Receipt) bool {
		return skip[delivery.Boot{Node: r.Node, Boot: r.Boot}]
	})
	byHop := map[delivery.Hop]map[delivery.Kind]delivery.Total{
		delivery.Node:   delivery.ReconcileNode(d.Window, settled),
		delivery.Ingest: delivery.Reconcile(d.Window, settled),
	}
	var rows []DeliveryRow
	for _, k := range delivery.Kinds {
		for _, h := range []delivery.Hop{delivery.Node, delivery.Ingest} {
			if t, ok := byHop[h][k]; ok {
				rows = append(rows, rowOf(h, k, t))
			}
		}
	}
	return rows, named, gone
}

// rowOf is one kind's total across one hop as a row: every outcome the hop
// has, counted.
func rowOf(h delivery.Hop, k delivery.Kind, t delivery.Total) DeliveryRow {
	row := DeliveryRow{Hop: h, Kind: k, In: t.In, Out: t.Out, Deduplicated: t.Deduplicated,
		Outcomes: map[delivery.Outcome]uint64{}, Cause: t.Cause()}
	for _, o := range delivery.OutcomesOf(h) {
		row.Outcomes[o] = t.Outcomes[o]
	}
	if l, ok := t.Loss(); ok {
		row.Loss = &l
	}
	return row
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

// EndToEndOf is each kind's loss over every hop it crosses, 1 - prod(1 -
// loss), for the kinds with a ratio on every row.
func EndToEndOf(rows []DeliveryRow) map[delivery.Kind]float64 {
	hops := map[delivery.Kind][]float64{}
	absent := map[delivery.Kind]bool{}
	for _, r := range rows {
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
// it was minted. Beside them, the first landed receipt of every other boot
// of the same nodes minted after from, which says whether a boot that never
// settled was ended by its node starting again (#1692).
func ReceiptsQuery(from, to time.Time) string {
	return fmt.Sprintf(`select row_to_json(r) from (
		with first_in as (
			select node, boot, min(seq) as seq from telemetry_receipt
			where ledger is not null and minted > to_timestamp(%[1]s) and minted <= to_timestamp(%[2]s)
			group by node, boot)
		select t.node, t.boot, t.seq, t.minted, t.resolved, t.deliveries, t.ledger, t.outcome, t.node_loss
		from telemetry_receipt t join first_in f on f.boot = t.boot
		where t.ledger is not null and (t.seq >= f.seq or t.seq = (
			select max(l.seq) from telemetry_receipt l
			where l.boot = f.boot and l.ledger is not null and l.seq < f.seq))
		union all
		select t.node, t.boot, t.seq, t.minted, t.resolved, t.deliveries, t.ledger, t.outcome, t.node_loss
		from telemetry_receipt t
		where t.ledger is not null and t.minted > to_timestamp(%[1]s)
			and t.node in (select node from first_in) and t.boot not in (select boot from first_in)
			and t.seq = (select min(n.seq) from telemetry_receipt n where n.boot = t.boot and n.ledger is not null)
		order by node, boot, seq) r`,
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
			NodeLoss   delivery.NodeLoss `json:"node_loss"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("read a receipt %q: %w", line, err)
		}
		r := delivery.Receipt{Boot: row.Boot, Seq: row.Seq, Minted: row.Minted.UTC(), Resolved: row.Resolved,
			Deliveries: row.Deliveries, Declared: row.Ledger, Outcome: row.Outcome, Loss: row.NodeLoss}
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

// deliveryTable is the report's delivery section: a row per kind and hop,
// the end-to-end line, then each unsettled boot by name with no ratio.
func (r ScaleReport) deliveryTable(b *strings.Builder) {
	if r.DeliveryNotRead != "" {
		fmt.Fprintf(b, "%-12s%s\n", "delivery", "not measured: "+r.DeliveryNotRead)
		return
	}
	row := func(cells ...string) {
		fmt.Fprintf(b, "%-12s%-8s%-9s%-9s%-9s%-7s%s\n", cells[0], cells[1], cells[2], cells[3], cells[4], cells[5], cells[6])
	}
	row("delivery", "hop", "in", "out", "loss", "dedup", "cause")
	missing := false
	for _, d := range r.Delivery {
		loss, cause := "-", "-"
		if d.Loss != nil {
			loss = ratio(*d.Loss)
		}
		if d.Cause != "" {
			cause = string(d.Cause)
			missing = missing || d.Cause == delivery.Missing
		}
		row(string(d.Kind), string(d.Hop), strconv.FormatUint(d.In, 10), strconv.FormatUint(d.Out, 10), loss,
			strconv.FormatUint(d.Deduplicated, 10), cause)
	}
	if len(r.Delivery) > 0 {
		cells := []string{"end to end"}
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
		fmt.Fprintf(b, "%-12snode %s, boot %s: no ratio\n", "unsettled", u.Node, u.Boot)
	}
	for _, e := range r.DeliveryEnded {
		fmt.Fprintf(b, "%-12snode %s, boot %s: last seq %d landed, boot %s began %s; anything after %d never shipped, uncounted\n",
			"unshipped", e.Node, e.Boot, e.Last, e.Next, e.NextMinted.UTC().Format(time.RFC3339), e.Last)
	}
	if missing {
		b.WriteString("missing: " + missingLabel + "\n")
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
