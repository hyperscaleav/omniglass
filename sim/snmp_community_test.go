package sim

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// A face's community is strict (#821): it answers its own community alone,
// so a test using any other (public included) meets the silence a real v2c
// agent gives a wrong community; and a refused community, one the agent
// knows but its access policy refuses, is answered with authorizationError.
func TestTheSNMPFaceIsStrictAndRefusesItsRefusedCommunity(t *testing.T) {
	dev := NewDevice("dev-1", "test-host", net.ParseIP("127.0.0.1"), "test-profile", 1,
		map[string]any{"status": map[string]any{"device": map[string]any{"uptime": 1234}}})
	dev.OIDMap = map[string]string{"1.3.6.1.2.1.1.3.0": "status.device.uptime"}
	dev.SetSNMPRefusedCommunity("revoked")
	bound, err := dev.startSNMP(0, "lab-ro")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dev.StopSNMP)
	target := bound.String()
	g := collection.NewSNMPGetter()
	ctx := context.Background()
	if _, err := g.Get(ctx, target, "lab-ro", []string{"1.3.6.1.2.1.1.3.0"}, time.Second); err != nil {
		t.Fatalf("the face's own community: %v", err)
	}
	if _, err := g.Get(ctx, target, "public", []string{"1.3.6.1.2.1.1.3.0"}, 300*time.Millisecond); err == nil {
		t.Fatal("a strict face answered public beside its own community")
	} else if _, terminal := collection.AsTerminal(err); terminal {
		t.Fatalf("a wrong community read as a refusal: %v", err)
	}
	_, err = g.Get(ctx, target, "revoked", []string{"1.3.6.1.2.1.1.3.0"}, time.Second)
	if terr, ok := collection.AsTerminal(err); !ok || terr.Class != "auth" {
		t.Fatalf("the refused community = %v, want an authorizationError read as auth", err)
	}
	if n := dev.SNMPRequests(); n != 3 {
		t.Fatalf("the face counted %d requests, want 3", n)
	}
}

// The catalog's v2c faces declare their refused community, so the
// simulated fleet can show an agent that refuses one (#821).
func TestTheCatalogsSNMPFacesRefuseTheirRefusedCommunity(t *testing.T) {
	_, d := panelEngine(t, "127.88.9.41")
	target := faceAddr(t, d, "snmp")
	g := collection.NewSNMPGetter()
	if _, err := g.Get(context.Background(), target, "public", []string{"1.3.6.1.2.1.1.3.0"}, time.Second); err != nil {
		t.Fatalf("the panel's own community: %v", err)
	}
	_, err := g.Get(context.Background(), target, "revoked", []string{"1.3.6.1.2.1.1.3.0"}, time.Second)
	if terr, ok := collection.AsTerminal(err); !ok || terr.Class != "auth" {
		t.Fatalf("the panel's refused community = %v, want authorizationError read as auth", err)
	}
}
