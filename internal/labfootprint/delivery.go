package labfootprint

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/closedloop"
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
	// Schedule is the schedule hop's runs due and made per kind (#1693).
	Schedule []ScheduleCount
}

// DeliveryRow is one kind across one hop: the same keys for every kind and
// hop, every outcome counted, and loss absent when nothing went in. A sim
// hop row is one protocol instead (#1694), counted in requests: the sim
// cannot know which kinds an answer would have carried.
type DeliveryRow struct {
	Hop          delivery.Hop                `json:"hop"`
	Kind         delivery.Kind               `json:"kind,omitempty"`
	Protocol     string                      `json:"protocol,omitempty"`
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

// SimDeliveryOf is the sim hop (#1694): one row per protocol the nodes
// tallied that the simulated endpoints settled requests on, in protocol
// order, summed over every node's closed-loop bracket. In is what the sims
// settled, out what came back to the node's tasks; a healthy lab reads 0.
func SimDeliveryOf(brackets []closedloop.NodeBracket) []DeliveryRow {
	var rows []DeliveryRow
	for _, c := range closedloop.SimHops(brackets) {
		t := delivery.Total{In: uint64(c.In), Out: uint64(c.In - c.Lost), Outcomes: map[delivery.Outcome]uint64{
			delivery.Returned: uint64(c.In - c.Lost), delivery.Withheld: uint64(c.Withheld),
			delivery.Dropped: uint64(c.Dropped), delivery.NotReturned: uint64(c.NotReturned),
		}}
		row := DeliveryRow{Hop: delivery.Sim, Protocol: c.Protocol, In: t.In, Out: t.Out, Outcomes: t.Outcomes, Cause: t.Cause()}
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

// EndToEndOf is each kind's loss over every hop it crosses, 1 - prod(1 -
// loss), for the kinds with a settled ingest row (the last hop; without it
// the kind's delivery is not known) and a ratio on some hop. A hop with
// nothing in has no ratio and loses nothing, so it composes as 0: a schedule
// that skipped every due leaves the ingest nothing to carry, and the kind
// reads the schedule's whole loss. The sim hop's rows are per protocol, not
// per kind, so they compose as one: the requests every protocol lost over the
// requests every protocol settled, read into every kind with a ratio of its
// own. That is a lost reply losing every item it would have carried, in
// proportion to the requests (ADR-1694).
func EndToEndOf(rows []DeliveryRow) map[delivery.Kind]float64 {
	hops := map[delivery.Kind][]float64{}
	ingested := map[delivery.Kind]bool{}
	var simIn, simOut uint64
	for _, r := range rows {
		if r.Hop == delivery.Sim {
			simIn, simOut = simIn+r.In, simOut+r.Out
			continue
		}
		ingested[r.Kind] = ingested[r.Kind] || r.Hop == delivery.Ingest
		if r.Loss != nil {
			hops[r.Kind] = append(hops[r.Kind], *r.Loss)
		}
	}
	if sim, ok := delivery.Loss(simIn, simOut); ok {
		for k := range hops {
			hops[k] = append(hops[k], sim)
		}
	}
	out := map[delivery.Kind]float64{}
	for k, ls := range hops {
		if ingested[k] {
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

// deliveryTable is the report's delivery section: a row per protocol across
// the sim hop, a row per kind across the ingest hop, the end-to-end line,
// then what the sim rows leave out, then each unsettled boot by name with no
// ratio.
func (r ScaleReport) deliveryTable(b *strings.Builder) {
	if r.DeliveryNotRead != "" && len(r.Delivery) == 0 {
		fmt.Fprintf(b, "%-12s%s\n", "delivery", "not measured: "+r.DeliveryNotRead)
		r.simGap(b)
		return
	}
	// The hop column fits its longest name with two spaces after it, eight
	// wide at least.
	hop := 8
	for _, d := range r.Delivery {
		hop = max(hop, len(d.Hop)+2)
	}
	row := func(cells ...string) {
		fmt.Fprintf(b, "%-12s%-*s%-9s%-9s%-9s%-7s%s\n", cells[0], hop, cells[1], cells[2], cells[3], cells[4], cells[5], cells[6])
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
		name, dedup := string(d.Kind), strconv.FormatUint(d.Deduplicated, 10)
		if d.Hop == delivery.Sim {
			// A sim row is a protocol, and the sim hop has no redelivery.
			name, dedup = d.Protocol, "-"
		}
		row(name, string(d.Hop), strconv.FormatUint(d.In, 10), strconv.FormatUint(d.Out, 10), loss, dedup, cause)
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
	if r.DeliveryNotRead != "" {
		fmt.Fprintf(b, "%-12s%s\n", "ingest", "not measured: "+r.DeliveryNotRead)
	}
	r.simGap(b)
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

// simGap says what the sim rows leave out: the whole hop when the closed
// loop was not read, or the nodes that restarted in the window, whose
// requests no sim row counts (the rows sum the other nodes).
func (r ScaleReport) simGap(b *strings.Builder) {
	if r.EndpointsGap != "" {
		fmt.Fprintf(b, "%-12s%s\n", "sim", "not measured: "+r.EndpointsGap)
		return
	}
	if r.Endpoints == nil {
		return
	}
	var restarted []string
	for _, n := range r.Endpoints.Nodes {
		if n.Restarted {
			restarted = append(restarted, n.Node)
		}
	}
	if len(restarted) > 0 {
		fmt.Fprintf(b, "%-12snot counted: %s restarted in the window\n", "sim", strings.Join(restarted, ", "))
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
