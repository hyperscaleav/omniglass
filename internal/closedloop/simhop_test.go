package closedloop

import (
	"reflect"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// The sim hop (#1694) is what the simulated endpoints settled against what
// the outpost got back, per protocol the outpost tallies: a healthy window reads
// every request returned, and each way a reply can fail to reach the outpost
// reads as its own outcome.
func TestSimHopCountsEachProtocolsReturns(t *testing.T) {
	for _, c := range []struct {
		name  string
		plant func(*OutpostBracket)
		want  []SimHopCount
	}{
		{"a healthy window returns everything", func(*OutpostBracket) {},
			[]SimHopCount{{Protocol: "http", In: 60}, {Protocol: "snmp", In: 30}}},
		{"a request the sim withheld", func(b *OutpostBracket) {
			endSims(b, func(o *collection.SimObservation) {
				p := protocol(o, "snmp")
				p.Received, p.Withheld = 90, 10
			})
			n := transportRow(&b.End.Outpost, "snmp")
			n.Sent, n.TimedOut = 90, 10
		}, []SimHopCount{{Protocol: "http", In: 60}, {Protocol: "snmp", In: 40, Lost: 10, Withheld: 10}}},
		{"a reply the sim could not write", func(b *OutpostBracket) {
			endSims(b, func(o *collection.SimObservation) {
				p := protocol(o, "http")
				p.Received, p.Dropped = 164, 4
			})
			n := transportRow(&b.End.Outpost, "http")
			n.Sent, n.TimedOut = 164, 4
		}, []SimHopCount{{Protocol: "http", In: 64, Lost: 4, Dropped: 4}, {Protocol: "snmp", In: 30}}},
		{"an answer the outpost never got back", func(b *OutpostBracket) {
			endSims(b, func(o *collection.SimObservation) {
				p := protocol(o, "snmp")
				p.Received, p.Answered = 90, 90
			})
			n := transportRow(&b.End.Outpost, "snmp")
			n.Sent, n.TimedOut = 90, 10
		}, []SimHopCount{{Protocol: "http", In: 60}, {Protocol: "snmp", In: 40, Lost: 10, NotReturned: 10}}},
		{"an answer still in flight at the outpost is not lost", func(b *OutpostBracket) {
			endSims(b, func(o *collection.SimObservation) {
				p := protocol(o, "snmp")
				p.Received, p.Answered = 85, 85
			})
			n := transportRow(&b.End.Outpost, "snmp")
			n.Sent, n.InFlight = 85, 5
		}, []SimHopCount{{Protocol: "http", In: 60}, {Protocol: "snmp", In: 35}}},
		{"traffic the outpost does not tally is not counted in", func(b *OutpostBracket) {
			endSims(b, func(o *collection.SimObservation) {
				p := protocol(o, "snmp")
				p.Received, p.Answered = 100, 100
			})
		}, []SimHopCount{{Protocol: "http", In: 60}, {Protocol: "snmp", In: 30}}},
		{"a refusal and an error reached the outpost", func(b *OutpostBracket) {
			endSims(b, func(o *collection.SimObservation) {
				p := protocol(o, "http")
				p.Received, p.Refused, p.Error = 165, 3, 2
			})
			n := transportRow(&b.End.Outpost, "http")
			n.Sent, n.Refused, n.Error = 165, 3, 2
		}, []SimHopCount{{Protocol: "http", In: 65}, {Protocol: "snmp", In: 30}}},
		{"a protocol the outpost does not tally reads nothing", func(b *OutpostBracket) {
			for _, o := range []*collection.SimObservation{&b.Start.SimBefore, &b.Start.SimAfter, &b.End.SimBefore, &b.End.SimAfter} {
				o.Protocols = append(o.Protocols, served("ssh", 5, 5))
			}
			endSims(b, func(o *collection.SimObservation) { protocol(o, "ssh").Received, protocol(o, "ssh").Withheld = 15, 10 })
		}, []SimHopCount{{Protocol: "http", In: 60}, {Protocol: "snmp", In: 30}}},
		{"a restarted outpost reads nothing", func(b *OutpostBracket) {
			b.End.Outpost.Started = b.End.Outpost.Started.Add(1)
		}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := balanced()
			c.plant(&b)
			if got := SimHop(b); !reflect.DeepEqual(got, c.want) {
				t.Errorf("SimHop = %+v\nwant %+v", got, c.want)
			}
		})
	}
}

// The window's sim hop sums every outpost's counts per protocol, in protocol
// order.
func TestSimHopsSumsTheOutposts(t *testing.T) {
	a, b := balanced(), balanced()
	b.Outpost = "west"
	endSims(&b, func(o *collection.SimObservation) {
		p := protocol(o, "snmp")
		p.Received, p.Withheld = 90, 10
	})
	n := transportRow(&b.End.Outpost, "snmp")
	n.Sent, n.TimedOut = 90, 10
	want := []SimHopCount{{Protocol: "http", In: 120}, {Protocol: "snmp", In: 70, Lost: 10, Withheld: 10}}
	if got := SimHops([]OutpostBracket{b, a}); !reflect.DeepEqual(got, want) {
		t.Errorf("SimHops = %+v\nwant %+v", got, want)
	}
	if got := SimHops(nil); got != nil {
		t.Errorf("SimHops(nil) = %+v, want nil", got)
	}
}
