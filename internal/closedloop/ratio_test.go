package closedloop

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// row is one gap's ratio row on a protocol ("" for a telemetry hop).
func row(rs []GapRatio, gap, protocol string) *GapRatio {
	for i := range rs {
		if rs[i].Gap == gap && rs[i].Protocol == protocol {
			return &rs[i]
		}
	}
	return nil
}

// A ratio is a count over the figure it was lost from; with nothing in,
// there is no ratio to read, never a zero and never a division by zero.
func TestRatio(t *testing.T) {
	if r, ok := Ratio(5, 30); !ok || r != 5.0/30 {
		t.Fatalf("Ratio(5, 30) = %v, %v", r, ok)
	}
	if r, ok := Ratio(0, 30); !ok || r != 0 {
		t.Fatalf("Ratio(0, 30) = %v, %v, want a healthy 0", r, ok)
	}
	if _, ok := Ratio(3, 0); ok {
		t.Fatal("Ratio(3, 0) read a ratio; want it absent")
	}
	if _, ok := Ratio(0, 0); ok {
		t.Fatal("Ratio(0, 0) read a ratio; want it absent")
	}
}

// A window in which every hop carried everything reads 0 on every gap that
// had something to lose, and names every gap, the zeroes Reconcile leaves
// out included.
func TestRatiosBalancedWindowReadsZero(t *testing.T) {
	rs := Ratios(balanced())
	named := map[string]bool{}
	for _, r := range rs {
		named[r.Gap] = true
		if r.Count != 0 || r.Reading != ReadingLoss {
			t.Errorf("%s %s = %+v, want no count, read as loss", r.Gap, r.Protocol, r)
		}
		if r.In > 0 && (r.Ratio == nil || *r.Ratio != 0) {
			t.Errorf("%s %s ratio = %v over %d, want 0", r.Gap, r.Protocol, r.Ratio, r.In)
		}
	}
	for _, g := range GapNames() {
		if !named[g] {
			t.Errorf("gap %s is not named: %+v", g, rs)
		}
	}
	for _, want := range []struct {
		gap, protocol string
		in            int64
	}{
		{GapConnectNotAccepted, "", 70},
		{GapSentNotReceived, "http", 60}, {GapSentNotReceived, "snmp", 30},
		{GapReceivedNotSettled, "http", 60}, {GapAnsweredNotReturned, "snmp", 30},
		{GapPublishedNotLanded, "", 6}, {GapSpoolDropped, "", 6}, {GapUnhitEndpoint, "http", 1},
	} {
		r := row(rs, want.gap, want.protocol)
		if r == nil || r.In != want.in {
			t.Errorf("%s %s = %+v, want in %d", want.gap, want.protocol, r, want.in)
		}
	}
}

// A planted shortfall reads its count over its own in figure, with the
// masking term beside it, never folded into the ratio.
func TestRatiosShortfallReadsCountOverIn(t *testing.T) {
	b := balanced()
	endSims(&b, func(o *collection.SimObservation) {
		p := protocol(o, "snmp")
		p.Received, p.Answered = 75, 75
	})
	b.Receipts = ReceiptCount{Landed: 4, Margin: 1, AfterEnd: 1}
	rs := Ratios(b)
	snmp := row(rs, GapSentNotReceived, "snmp")
	if snmp == nil || snmp.Count != 5 || snmp.In != 30 || snmp.Ratio == nil || *snmp.Ratio != 5.0/30 {
		t.Fatalf("snmp sent-not-received = %+v, want 5 of 30", snmp)
	}
	landed := row(rs, GapPublishedNotLanded, "")
	if landed == nil || landed.Count != 2 || landed.In != 6 || *landed.Ratio != 2.0/6 || landed.Masked != 2 || landed.MaskedBy != maskedByReceiptEdges {
		t.Fatalf("published-not-landed = %+v, want 2 of 6 with 2 masked beside it", landed)
	}
}

// Unhit counts a component once per protocol however many of its poll
// tasks land there, so its in figure does too: a component whose two http
// polls both drew nothing is one wholly unhit endpoint, never half of two.
func TestRatiosUnhitCountsAComponentOncePerProtocol(t *testing.T) {
	b := balanced()
	b.Targets = append(b.Targets, PollTarget{ComponentID: "c-tls", Transport: "https", Interval: 2e9},
		PollTarget{ComponentID: "c-tls", Transport: "http", Interval: 2e9})
	endSims(&b, func(o *collection.SimObservation) {
		p := protocol(o, "http")
		p.Unhit, p.UnhitSample = 1, []string{"c-tls"}
	})
	rs := Ratios(b)
	if u := row(rs, GapUnhitEndpoint, "http"); u == nil || u.Count != 1 || u.In != 2 || *u.Ratio != 0.5 {
		t.Fatalf("http unhit = %+v, want 1 of 2 components", u)
	}
}

// A gap with nothing in reads absent: tcp sends no request, so its
// sent-not-received has no ratio, and the JSON says null.
func TestRatiosNothingInReadsAbsent(t *testing.T) {
	rs := Ratios(balanced())
	tcp := row(rs, GapSentNotReceived, "tcp")
	if tcp == nil || tcp.In != 0 || tcp.Ratio != nil {
		t.Fatalf("tcp sent-not-received = %+v, want no ratio", tcp)
	}
	raw, err := json.Marshal(tcp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"ratio":null`) {
		t.Fatalf("json = %s, want a null ratio", raw)
	}
}

// Unhit endpoints read per protocol, over the components due on it; icmp reads
// not observed by the sim; a restarted node reads its one entry, no ratio.
func TestRatiosUnhitAndRestarted(t *testing.T) {
	b := balanced()
	b.Targets = append(b.Targets, PollTarget{ComponentID: "c-tls", Transport: "https", Interval: 2e9},
		PollTarget{ComponentID: "c-ping", Transport: "icmp", Interval: 2e9})
	endSims(&b, func(o *collection.SimObservation) {
		p := protocol(o, "http")
		p.Unhit, p.UnhitSample = 1, []string{"c-tls"}
	})
	rs := Ratios(b)
	if u := row(rs, GapUnhitEndpoint, "http"); u == nil || u.Count != 1 || u.In != 2 || *u.Ratio != 0.5 {
		t.Fatalf("http unhit = %+v, want 1 of 2 targets", u)
	}
	if u := row(rs, GapUnhitEndpoint, "icmp"); u == nil || u.Reading != ReadingNotObserved || u.In != 1 {
		t.Fatalf("icmp unhit = %+v, want its one target not observed", u)
	}

	b = balanced()
	b.End.Node.BatchesAcknowledged = 2
	rs = Ratios(b)
	if len(rs) != 1 || rs[0].Reading != ReadingRestarted || rs[0].Ratio != nil {
		t.Fatalf("restarted = %+v, want one entry with no ratio", rs)
	}
}
