package labfootprint

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/closedloop"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/sim"
)

// The sim hop (#1694) against real sockets: a simulated SNMP agent and the
// node's real tallied getter, bracketed the way the scale lab brackets them.
// A healthy window reads loss 0; an agent that withholds its answers (a
// community it does not know, so it stays silent) reads every withheld
// request as the hop's loss, and names withheld as the cause.
func TestSimHopReadsWithheldAnswersAsLossOverRealSockets(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	for _, c := range []struct {
		name          string
		right, wrong  int
		wantIn, wantW uint64
	}{
		{"a healthy agent loses nothing", 6, 0, 6, 0},
		{"an agent that withholds answers", 4, 3, 7, 3},
	} {
		t.Run(c.name, func(t *testing.T) {
			dev := sim.NewDevice("d", "sim-hop-host", net.ParseIP("127.0.0.1"), "p", 1,
				map[string]any{"status": map[string]any{"device": map[string]any{"uptime": 1234}}})
			dev.OIDMap = map[string]string{"1.3.6.1.2.1.1.3.0": "status.device.uptime"}
			if err := dev.StartSNMP(0, "lab-ro"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(dev.StopSNMP)
			tally := collection.NewRequestTally()
			g := collection.NewTalliedSNMPGetter(tally)
			target := dev.SNMPAddr().String()

			start := bracketRead(dev, tally)
			ctx := context.Background()
			for range c.right {
				if _, err := g.Get(ctx, target, "lab-ro", []string{"1.3.6.1.2.1.1.3.0"}, 2*time.Second); err != nil {
					t.Fatalf("a get with the agent's community: %v", err)
				}
			}
			for range c.wrong {
				if _, err := g.Get(ctx, target, "not-the-community", []string{"1.3.6.1.2.1.1.3.0"}, 200*time.Millisecond); err == nil {
					t.Fatal("a get with a community the agent does not know was answered")
				}
			}
			settledBy(t, dev, int64(c.right+c.wrong))
			end := bracketRead(dev, tally)

			rows := SimDeliveryOf([]closedloop.NodeBracket{{Node: "edge", Start: start, End: end, Window: time.Minute}})
			if len(rows) != 1 {
				t.Fatalf("sim rows = %+v, want one snmp row", rows)
			}
			r := rows[0]
			if r.Hop != delivery.Sim || r.Protocol != "snmp" || r.In != c.wantIn || r.Out != c.wantIn-c.wantW ||
				r.Outcomes[delivery.Withheld] != c.wantW || r.Loss == nil {
				t.Fatalf("the snmp sim row = %+v, want %d in, %d withheld", r, c.wantIn, c.wantW)
			}
			want, _ := delivery.Loss(c.wantIn, c.wantIn-c.wantW)
			if *r.Loss != want {
				t.Errorf("loss %v, want %v", *r.Loss, want)
			}
			if c.wantW > 0 && r.Cause != delivery.Withheld {
				t.Errorf("cause %q, want withheld", r.Cause)
			}
			if c.wantW == 0 && r.Cause != "" {
				t.Errorf("a healthy agent reads cause %q", r.Cause)
			}
		})
	}
}

// bracketRead is one closed-loop bracket over a lone device: the sim, the
// node's tally, the sim again.
func bracketRead(dev *sim.Device, tally *collection.RequestTally) collection.EndpointObservation {
	before := simRead(dev)
	node := tally.Snapshot(time.Now())
	return collection.EndpointObservation{SimBefore: before, Node: node, SimAfter: simRead(dev)}
}

// simRead is the device's snmp counters in the wire form the server reads.
func simRead(dev *sim.Device) collection.SimObservation {
	c := dev.Observed("snmp")
	return collection.SimObservation{Node: "edge", At: time.Now(), Devices: 1, Protocols: []collection.SimProtocolObservation{{
		Protocol: "snmp", Accepted: c.Accepted, Received: c.Received, InFlight: c.InFlight, Answered: c.Answered,
		Refused: c.Refused, Error: c.Error, Dropped: c.Dropped, Withheld: c.Withheld, Served: 1,
	}}}
}

// settledBy waits for the device to have settled n requests.
func settledBy(t *testing.T, dev *sim.Device, n int64) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for dev.Observed("snmp").Settled() < n {
		if time.Now().After(deadline) {
			t.Fatalf("the agent settled %+v, want %d", dev.Observed("snmp"), n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
