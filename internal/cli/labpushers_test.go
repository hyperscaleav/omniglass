package cli

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/lab"
	labscenario "github.com/hyperscaleav/omniglass/internal/lab/scenario"
)

// fakeMint mints a distinct, recognizable token each call.
func fakeMint() func() (string, []byte, string, error) {
	n := 0
	return func() (string, []byte, string, error) {
		n++
		return fmt.Sprintf("tok-%d", n), []byte{byte(n), 0xab}, fmt.Sprintf("pfx%d", n), nil
	}
}

// The scale plan provisions one principal and token per declared pusher
// (#1743): a corporate lab's every pusher, on its own site's node, with a
// token of its own whose hash is what the seed sets, and each one handed to
// the seed by name.
func TestScaleLabMintsATokenPerDeclaredPusher(t *testing.T) {
	m, err := labscenario.Shipped("corporate")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := labscenario.ShippedCatalog()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := labscenario.Resolve(m, cat, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{} // pusher -> its site's node
	for _, site := range plan.Sites {
		for _, p := range site.Pushers {
			if p.Webhook {
				continue // a meeting platform signs with a seeded secret, holding no token (#1747)
			}
			want[p.Name] = labscenario.SiteNode(site.Name)
		}
	}
	if len(want) != len(plan.Sites) {
		t.Fatalf("corporate at 100 declares %d pushers over %d sites, want one a site", len(want), len(plan.Sites))
	}
	ps, err := scaleLabPushers(plan, fakeMint())
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != len(want) {
		t.Fatalf("minted %d pushers, want %d", len(ps), len(want))
	}
	tokens := map[string]bool{}
	for _, p := range ps {
		if node, ok := want[p.Name]; !ok || p.Node != node {
			t.Errorf("pusher %s on node %s, want it declared and on %s", p.Name, p.Node, node)
		}
		if p.Profile != "booking-service" {
			t.Errorf("pusher %s runs %q, want booking-service", p.Name, p.Profile)
		}
		if p.Token == "" || tokens[p.Token] {
			t.Errorf("pusher %s token %q, want one of its own", p.Name, p.Token)
		}
		tokens[p.Token] = true
		if _, err := hex.DecodeString(p.TokenHash); err != nil || p.TokenHash == "" || p.TokenPrefix == "" {
			t.Errorf("pusher %s hash %q prefix %q, want hex and a prefix", p.Name, p.TokenHash, p.TokenPrefix)
		}
	}
	got, err := lab.ParsePusherTokens(lab.FormatPusherTokens(ps))
	if err != nil || len(got) != len(want) {
		t.Fatalf("the seed reads %d pusher tokens (%v), want %d", len(got), err, len(want))
	}
	if strings.Contains(lab.FormatPusherTokens(ps), "tok-") {
		t.Error("the seed's pusher tokens carry a plaintext token")
	}
}

// The dev lab's pushers are its fleet's declarations, minted the same way
// onto its one node; a lab with no node runs none, so it mints none.
func TestDevLabMintsATokenPerDeclaredPusher(t *testing.T) {
	doc, err := devseed.Fixtures()
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.SimPushers) == 0 {
		t.Fatal("the dev fleet declares no pusher")
	}
	ps, err := devLabPushers(doc.SimPushers, false, fakeMint())
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != len(doc.SimPushers) {
		t.Fatalf("minted %d, want %d", len(ps), len(doc.SimPushers))
	}
	for i, p := range ps {
		d := doc.SimPushers[i]
		if p.Name != d.Name || p.Profile != d.Profile || p.Node != lab.NodeName || p.Token == "" || p.TokenHash == "" {
			t.Errorf("pusher %d = %+v, want %s (%s) on %s with a token", i, p, d.Name, d.Profile, lab.NodeName)
		}
	}
	none, err := devLabPushers(doc.SimPushers, true, fakeMint())
	if err != nil || len(none) != 0 {
		t.Errorf("with no node minted %v (%v), want none", none, err)
	}
}
