package sim

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// An SNMP face settles each datagram: its own community is answered, its
// refused community refused, and any other withheld (the silence a real v2c
// agent gives a wrong community).
func TestObservedSNMPDatagramsSettleByCommunity(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket test")
	}
	dev := NewDevice("dev-1", "h", net.ParseIP("127.0.0.1"), "p", 1,
		map[string]any{"status": map[string]any{"device": map[string]any{"uptime": 1234}}})
	dev.OIDMap = map[string]string{"1.3.6.1.2.1.1.3.0": "status.device.uptime"}
	dev.SetSNMPRefusedCommunity("revoked")
	bound, err := dev.startSNMP(0, "lab-ro")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dev.StopSNMP)
	g := collection.NewSNMPGetter()
	ctx := context.Background()
	oid := []string{"1.3.6.1.2.1.1.3.0"}
	if _, err := g.Get(ctx, bound.String(), "lab-ro", oid, time.Second); err != nil {
		t.Fatal(err)
	}
	// The face counts a datagram after writing its reply, so the client can
	// hold the reply before the count lands.
	c := observedEventually(t, dev, "snmp", func(c ObservationCounters) bool { return c.Settled() == 1 })
	if c.Received != 1 || c.Answered != 1 || c.InFlight != 0 || c.Accepted != 0 {
		t.Fatalf("one answered get = %+v", c)
	}
	_, _ = g.Get(ctx, bound.String(), "revoked", oid, time.Second)
	_, _ = g.Get(ctx, bound.String(), "public", oid, 200*time.Millisecond)
	c = observedEventually(t, dev, "snmp", func(c ObservationCounters) bool { return c.Settled() == 3 })
	if c.Refused != 1 || c.Withheld != 1 || c.Received != 3 {
		t.Fatalf("a refused and a wrong community = %+v, want one refused and one withheld", c)
	}
	if dev.SNMPRequests() != 3 {
		t.Fatalf("SNMPRequests = %d, want 3", dev.SNMPRequests())
	}
}
