package seed

import "testing"

// The Tessellate Census's raw-TCP band opens on a welcome banner (#2757), so a
// one-shot exchange would read the banner as the answer to COUNT?. The feed
// driver therefore stands a listener on the count's push, which is what makes
// the outpost hold a session its poll rides, where the reply shape picks the
// answer out of the banner.
func TestTheShippedCensusFeedDriverHoldsASessionForItsPoll(t *testing.T) {
	sp := shippedSpec(t, "tessellate-census-feed")
	if err := sp.Validate(nil); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if sp.Transport != "tcp" || len(sp.Polls) != 1 || sp.Polls[0].Request.Line != "COUNT?" {
		t.Fatalf("transport %q polls %+v, want one COUNT? poll over tcp", sp.Transport, sp.Polls)
	}
	if r := sp.Polls[0].Request.Reply; r == nil || !r.Accepts("#COUNT=4") || r.Accepts("TESSELLATE CENSUS C1 FEED READY") {
		t.Errorf("the poll's reply shape must take the count and leave the banner")
	}
	if len(sp.Listeners) == 0 {
		t.Fatal("the driver has no listener, so its poll would ride a one-shot exchange and read the banner")
	}
}
