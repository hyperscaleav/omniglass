package closedloop

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

var (
	simStarted     = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	outpostStarted = simStarted.Add(time.Second)
	windowStart    = simStarted.Add(time.Minute)
	windowEnd      = windowStart.Add(2 * time.Minute)
)

// served is one protocol's sim row with every request answered.
func served(p string, accepted, received int64) collection.SimProtocolObservation {
	return collection.SimProtocolObservation{Protocol: p, Accepted: accepted, Received: received, Answered: received, Served: 3}
}

// sent is one transport's outpost row with every request answered.
func sent(tr string, connections, n int64) collection.TallyCounters {
	return collection.TallyCounters{Transport: tr, Connections: connections, Sent: n, Answered: n}
}

func simRead(at time.Time, rows ...collection.SimProtocolObservation) collection.SimObservation {
	return collection.SimObservation{Outpost: "edge", At: at, Started: simStarted, FleetGeneration: 1, Devices: 3, Protocols: rows}
}

// balanced is a two-minute window in which every hop carried everything:
// 60 http requests on 60 connections, 30 snmp datagrams, 10 tcp connects and
// 6 batches acknowledged and landed.
func balanced() OutpostBracket {
	start := collection.EndpointObservation{
		SimBefore: simRead(windowStart, served("http", 100, 100), served("snmp", 0, 50), served("tcp", 10, 0)),
		Outpost: collection.RequestTallySnapshot{Outpost: "edge", At: windowStart, Started: outpostStarted, BatchesAcknowledged: 10,
			Transports: []collection.TallyCounters{sent("http", 100, 100), sent("snmp", 0, 50), {Transport: "tcp", Connections: 10}}},
		SimAfter: simRead(windowStart, served("http", 100, 100), served("snmp", 0, 50), served("tcp", 10, 0)),
	}
	end := collection.EndpointObservation{
		SimBefore: simRead(windowEnd, served("http", 160, 160), served("snmp", 0, 80), served("tcp", 20, 0)),
		Outpost: collection.RequestTallySnapshot{Outpost: "edge", At: windowEnd, Started: outpostStarted, BatchesAcknowledged: 16,
			Transports: []collection.TallyCounters{sent("http", 160, 160), sent("snmp", 0, 80), {Transport: "tcp", Connections: 20}}},
		SimAfter: simRead(windowEnd, served("http", 160, 160), served("snmp", 0, 80), served("tcp", 20, 0)),
	}
	return OutpostBracket{
		Outpost: "edge", Start: start, End: end, Window: 2 * time.Minute,
		Receipts: ReceiptCount{Landed: 6},
		Targets: []PollTarget{
			{ComponentID: "c-http", Transport: "http", Interval: 2 * time.Second},
			{ComponentID: "c-snmp", Transport: "snmp", Interval: 10 * time.Second},
		},
	}
}

// protocol returns a pointer to one protocol's row in a sim read.
func protocol(o *collection.SimObservation, p string) *collection.SimProtocolObservation {
	for i := range o.Protocols {
		if o.Protocols[i].Protocol == p {
			return &o.Protocols[i]
		}
	}
	panic("no row " + p)
}

// transportRow returns a pointer to one transport's row in an outpost read.
func transportRow(n *collection.RequestTallySnapshot, tr string) *collection.TallyCounters {
	for i := range n.Transports {
		if n.Transports[i].Transport == tr {
			return &n.Transports[i]
		}
	}
	panic("no row " + tr)
}

// endSims applies f to both of the end bracket's sim reads.
func endSims(b *OutpostBracket, f func(*collection.SimObservation)) {
	f(&b.End.SimBefore)
	f(&b.End.SimAfter)
}

func describe(gs []ClosedLoopGap) string {
	var s []string
	for _, g := range gs {
		s = append(s, fmt.Sprintf("%+v", g))
	}
	return "[" + strings.Join(s, "; ") + "]"
}

// losses is the gaps that read as loss with a count.
func losses(gs []ClosedLoopGap) []ClosedLoopGap {
	var out []ClosedLoopGap
	for _, g := range gs {
		if g.Reading == ReadingLoss && g.Count > 0 {
			out = append(out, g)
		}
	}
	return out
}

// Each hop's gap is a lower bound: it is positive only where something was
// lost, and the term that can hide a loss prints beside it.
func TestReconcileNamesEachHopsGap(t *testing.T) {
	for _, c := range []struct {
		name  string
		plant func(*OutpostBracket)
		want  ClosedLoopGap
	}{
		{"a connection the sim never accepted", func(b *OutpostBracket) {
			endSims(b, func(o *collection.SimObservation) { protocol(o, "http").Accepted = 157 })
		}, ClosedLoopGap{Gap: GapConnectNotAccepted, Count: 3, MaskedBy: maskedByBacklog}},
		{"a tcp connect the sim never accepted", func(b *OutpostBracket) {
			endSims(b, func(o *collection.SimObservation) { protocol(o, "tcp").Accepted = 18 })
		}, ClosedLoopGap{Gap: GapConnectNotAccepted, Count: 2, MaskedBy: maskedByBacklog}},
		{"a request the sim never received", func(b *OutpostBracket) {
			endSims(b, func(o *collection.SimObservation) {
				p := protocol(o, "snmp")
				p.Received, p.Answered = 75, 75
			})
		}, ClosedLoopGap{Gap: GapSentNotReceived, Protocol: "snmp", Count: 5, MaskedBy: maskedByOutpostInFlight}},
		{"a request the sim never settled", func(b *OutpostBracket) {
			endSims(b, func(o *collection.SimObservation) { protocol(o, "http").Answered = 157 })
		}, ClosedLoopGap{Gap: GapReceivedNotSettled, Protocol: "http", Count: 3}},
		{"an answer the outpost never got back", func(b *OutpostBracket) {
			transportRow(&b.End.Outpost, "http").Answered = 157
		}, ClosedLoopGap{Gap: GapAnsweredNotReturned, Protocol: "http", Count: 3, MaskedBy: maskedByReturn}},
		{"a batch acknowledged that never landed", func(b *OutpostBracket) {
			b.Receipts = ReceiptCount{Landed: 4, Margin: 1, Backlog: 3, AfterEnd: 1}
		}, ClosedLoopGap{Gap: GapPublishedNotLanded, Count: 2, Masked: 5, MaskedBy: maskedByReceiptEdges}},
		{"a batch the spool dropped", func(b *OutpostBracket) {
			b.End.Outpost.BatchesDropped = 2
		}, ClosedLoopGap{Gap: GapSpoolDropped, Count: 2}},
		{"a poll target that drew no request", func(b *OutpostBracket) {
			endSims(b, func(o *collection.SimObservation) {
				p := protocol(o, "http")
				p.Unhit, p.UnhitSample = 1, []string{"c-http"}
			})
		}, ClosedLoopGap{Gap: GapUnhitEndpoint, Protocol: "http", Count: 1, ComponentID: "c-http"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := balanced()
			c.plant(&b)
			got := losses(Reconcile(b))
			c.want.Outpost, c.want.Reading = "edge", ReadingLoss
			if len(got) != 1 || got[0] != c.want {
				t.Fatalf("gaps = %s, want only %+v", describe(got), c.want)
			}
		})
	}
}

// A request the sim left unsettled before the window is that window's loss,
// not this one's: the counters only grow, so a since-start residual would
// read one old leak in every window after it. Of the start's two sim reads,
// the larger residual is subtracted.
func TestReconcileAResidualUnsettledBeforeTheWindowIsNoGap(t *testing.T) {
	b := balanced()
	// 3 http requests went unsettled before the window; the start's first
	// read caught one of them still in flight.
	before := protocol(&b.Start.SimBefore, "http")
	before.Answered, before.InFlight = 97, 1
	protocol(&b.Start.SimAfter, "http").Answered = 97
	endSims(&b, func(o *collection.SimObservation) { protocol(o, "http").Answered = 157 })
	if l := losses(Reconcile(b)); len(l) != 0 {
		t.Fatalf("a leak from before the window read as this window's: %s", describe(l))
	}
	// Two more left unsettled inside the window are the gap.
	endSims(&b, func(o *collection.SimObservation) { protocol(o, "http").Answered = 155 })
	want := ClosedLoopGap{Gap: GapReceivedNotSettled, Outpost: "edge", Protocol: "http", Count: 2, Reading: ReadingLoss}
	if l := losses(Reconcile(b)); len(l) != 1 || l[0] != want {
		t.Fatalf("gaps = %s, want only %+v", describe(l), want)
	}
}

// When every hop carried everything there is nothing to report.
func TestReconcileBalancedFiguresYieldNoGap(t *testing.T) {
	if got := Reconcile(balanced()); len(got) != 0 {
		t.Fatalf("balanced figures read %s", describe(got))
	}
}

// A connection is an L4 fact: the outpost knows the transport it dialed and
// the sim knows the listener that accepted it, and the two need not agree (a
// tcp reachability probe on a port the sim serves http on, an http probe of a
// websocket face). So connections pool across every listener: one the outpost
// opened that some listener accepted is no loss (#1647), and the hop reads
// one outpost-wide row.
func TestReconcileAConnectAcceptedOnAnotherListenerIsNoLoss(t *testing.T) {
	b := balanced()
	// Two tcp probes the outpost opened landed on the sim's http listener, and
	// one more on a listener whose traffic the outpost does not tally.
	transportRow(&b.End.Outpost, "tcp").Connections = 23
	endSims(&b, func(o *collection.SimObservation) {
		protocol(o, "http").Accepted = 162
		o.Protocols = append(o.Protocols, collection.SimProtocolObservation{Protocol: "ssh", Accepted: 1})
	})
	if l := losses(Reconcile(b)); len(l) != 0 {
		t.Fatalf("connects accepted on another listener read as loss: %s", describe(l))
	}
	// One more the outpost opened that no listener accepted is the gap.
	transportRow(&b.End.Outpost, "tcp").Connections = 24
	want := ClosedLoopGap{Gap: GapConnectNotAccepted, Outpost: "edge", Count: 1, MaskedBy: maskedByBacklog, Reading: ReadingLoss}
	if l := losses(Reconcile(b)); len(l) != 1 || l[0] != want {
		t.Fatalf("gaps = %s, want only %+v", describe(l), want)
	}
}

// A request in flight at either read is in transit, not lost: it is the
// masking term, never the gap.
func TestReconcileRequestsInFlightAtEitherReadAreNoGap(t *testing.T) {
	b := balanced()
	// At the start, 5 http requests are out: the sim holds them too.
	n := transportRow(&b.Start.Outpost, "http")
	n.Sent, n.InFlight = 105, 5
	for _, o := range []*collection.SimObservation{&b.Start.SimBefore, &b.Start.SimAfter} {
		p := protocol(o, "http")
		p.Received, p.InFlight = 105, 5
	}
	// At the end, 5 more are out: 3 of them have not reached the sim yet,
	// 2 are being served, and one answer is on its way back.
	n = transportRow(&b.End.Outpost, "http")
	n.Sent, n.Answered, n.InFlight = 165, 160, 5
	for _, o := range []*collection.SimObservation{&b.End.SimBefore, &b.End.SimAfter} {
		p := protocol(o, "http")
		p.Received, p.Answered, p.InFlight = 162, 161, 1
	}
	// One snmp datagram the sim answered came back after the outpost gave up.
	n = transportRow(&b.End.Outpost, "snmp")
	n.Answered, n.TimedOut = 79, 1
	gs := Reconcile(b)
	if l := losses(gs); len(l) != 0 {
		t.Fatalf("requests in flight read as loss: %s", describe(l))
	}
	late := find(gs, GapAnsweredNotReturned, "snmp")
	if late == nil || late.AnsweredLate != 1 || late.Masked != 1 {
		t.Fatalf("the late snmp answer = %+v, want it printed beside its gap", late)
	}
	inflight := find(gs, GapSentNotReceived, "http")
	if inflight == nil || inflight.Masked != 5 {
		t.Fatalf("http sent-not-received = %+v, want the outpost's 5 in flight as its masking term", inflight)
	}
}

func find(gs []ClosedLoopGap, gap, protocol string) *ClosedLoopGap {
	for i := range gs {
		if gs[i].Gap == gap && gs[i].Protocol == protocol {
			return &gs[i]
		}
	}
	return nil
}

// The kernel answers icmp, so the sim never sees it: an icmp target is named
// as not observed, never as unhit.
func TestReconcileIcmpIsNotObservedBySim(t *testing.T) {
	b := balanced()
	b.Targets = append(b.Targets, PollTarget{ComponentID: "c-ping", Transport: "icmp", Interval: 2 * time.Second},
		PollTarget{ComponentID: "c-ping2", Transport: "icmp", Interval: 2 * time.Second})
	gs := Reconcile(b)
	if l := losses(gs); len(l) != 0 {
		t.Fatalf("icmp read as loss: %s", describe(l))
	}
	g := find(gs, GapUnhitEndpoint, "icmp")
	if g == nil || g.Reading != ReadingNotObserved || g.Count != 2 {
		t.Fatalf("icmp = %+v, want its 2 targets read %q", g, ReadingNotObserved)
	}
}

// A target is unhit only when the window should have drawn it twice; an
// unhit device the sample could not name is the masking term; a target the
// sim does not name unhit, or on a transport no outpost dials, is no gap.
func TestReconcileUnhitTargets(t *testing.T) {
	b := balanced()
	b.Targets = append(b.Targets,
		PollTarget{ComponentID: "c-slow", Transport: "http", Interval: 5 * time.Minute},
		PollTarget{ComponentID: "c-tls", Transport: "https", Interval: time.Second},
		PollTarget{ComponentID: "c-hook", Transport: "webhook", Interval: time.Second})
	endSims(&b, func(o *collection.SimObservation) {
		p := protocol(o, "http")
		p.Unhit, p.UnhitSample = 25, []string{"c-slow", "c-tls"}
		for len(p.UnhitSample) < collection.SimObservationSampleCap {
			p.UnhitSample = append(p.UnhitSample, fmt.Sprintf("other-%d", len(p.UnhitSample)))
		}
	})
	gs := Reconcile(b)
	l := losses(gs)
	if len(l) != 1 || l[0].ComponentID != "c-tls" || l[0].Protocol != "http" {
		t.Fatalf("unhit = %s, want only the https target, as http", describe(l))
	}
	var masked int64
	for _, g := range gs {
		if g.Gap == GapUnhitEndpoint && g.Protocol == "http" {
			masked += g.Masked
		}
	}
	if masked != 5 {
		t.Fatalf("unhit masking = %d, want the 5 unhit devices past the sample", masked)
	}
}

// A kernel impairment loses traffic below the sim, so on an outpost whose sim
// shows one active, a gap reads impaired with its count, not loss.
func TestReconcileImpairedOutpostReadsImpaired(t *testing.T) {
	b := balanced()
	b.End.SimAfter.Impaired = 1
	endSims(&b, func(o *collection.SimObservation) {
		p := protocol(o, "snmp")
		p.Received, p.Answered = 75, 75
	})
	gs := Reconcile(b)
	if l := losses(gs); len(l) != 0 {
		t.Fatalf("an impaired gap read as loss: %s", describe(l))
	}
	g := find(gs, GapSentNotReceived, "snmp")
	if g == nil || g.Reading != ReadingImpaired || g.Count != 5 {
		t.Fatalf("snmp = %+v, want 5 read %q", g, ReadingImpaired)
	}
}

// A counter that went backwards is a restart: the outpost reads restarted,
// once, and never a negative or a made-up gap.
func TestReconcileCounterResetReadsRestarted(t *testing.T) {
	for _, c := range []struct {
		name  string
		plant func(*OutpostBracket)
	}{
		{"the outpost restarted", func(b *OutpostBracket) {
			b.End.Outpost.Started = windowEnd.Add(-time.Second)
			transportRow(&b.End.Outpost, "http").Sent = 3
		}},
		{"the sim restarted", func(b *OutpostBracket) {
			endSims(b, func(o *collection.SimObservation) { o.Started = windowEnd.Add(-time.Second) })
		}},
		{"a sim counter went backwards", func(b *OutpostBracket) {
			endSims(b, func(o *collection.SimObservation) {
				p := protocol(o, "http")
				p.Received, p.Answered = 90, 90
			})
		}},
		{"the batches went backwards", func(b *OutpostBracket) { b.End.Outpost.BatchesAcknowledged = 2 }},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := balanced()
			c.plant(&b)
			gs := Reconcile(b)
			if len(gs) != 1 || gs[0].Reading != ReadingRestarted || gs[0].Outpost != "edge" {
				t.Fatalf("gaps = %s, want one %q for the outpost", describe(gs), ReadingRestarted)
			}
		})
	}
}

// A window over which the sim's served fleet did not hold still reads
// restarted (#1895): a request sent to a device the sim had not bound yet, or
// had just stopped, is sent and never received, and is no loss of the path.
// A fleet that held one generation throughout, the reads between the ends
// included, measures as usual.
func TestReconcileFleetChangeReadsRestarted(t *testing.T) {
	mid := func(gen int64, changing bool) collection.SimObservation {
		o := simRead(windowStart.Add(time.Minute), served("http", 130, 130), served("snmp", 0, 65), served("tcp", 15, 0))
		o.FleetGeneration, o.FleetChanging = gen, changing
		return o
	}
	for _, c := range []struct {
		name  string
		plant func(*OutpostBracket)
	}{
		{"the sim had not synced its fleet yet", func(b *OutpostBracket) {
			for _, o := range []*collection.SimObservation{&b.Start.SimBefore, &b.Start.SimAfter, &b.End.SimBefore, &b.End.SimAfter} {
				o.FleetGeneration = 0
			}
		}},
		{"the fleet changed between the ends", func(b *OutpostBracket) {
			endSims(b, func(o *collection.SimObservation) { o.FleetGeneration = 2 })
		}},
		{"a sync was changing the fleet at an end", func(b *OutpostBracket) { b.Start.SimAfter.FleetChanging = true }},
		{"a sync was changing the fleet between the ends", func(b *OutpostBracket) {
			b.Between = []collection.SimObservation{mid(1, false), mid(1, true)}
		}},
		{"the fleet changed and the ends agree only by the read between", func(b *OutpostBracket) {
			b.Between = []collection.SimObservation{mid(2, false)}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := balanced()
			c.plant(&b)
			gs := Reconcile(b)
			if len(gs) != 1 || gs[0].Reading != ReadingRestarted || gs[0].Outpost != "edge" {
				t.Fatalf("gaps = %s, want one %q for the outpost", describe(gs), ReadingRestarted)
			}
		})
	}
	b := balanced()
	b.Between = []collection.SimObservation{mid(1, false), mid(1, false)}
	for _, g := range Reconcile(b) {
		if g.Reading != ReadingLoss || g.Count != 0 {
			t.Fatalf("a steady fleet read %+v, want a healthy 0", g)
		}
	}
}

// A protocol the outpost does not tally yet (a session transport) reads its
// sim traffic as untallied, never as answers the outpost lost.
func TestReconcileUntalliedProtocolIsNoGap(t *testing.T) {
	b := balanced()
	b.Start.SimBefore.Protocols = append(b.Start.SimBefore.Protocols, served("ssh", 4, 10))
	b.Start.SimAfter.Protocols = append(b.Start.SimAfter.Protocols, served("ssh", 4, 10))
	endSims(&b, func(o *collection.SimObservation) { o.Protocols = append(o.Protocols, served("ssh", 6, 40)) })
	// An http session the outpost does not tally rides the same face.
	endSims(&b, func(o *collection.SimObservation) {
		p := protocol(o, "http")
		p.Accepted, p.Received, p.Answered = 161, 170, 170
	})
	if l := losses(Reconcile(b)); len(l) != 0 {
		t.Fatalf("untallied traffic read as loss: %s", describe(l))
	}
}
