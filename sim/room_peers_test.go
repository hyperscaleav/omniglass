package sim

import "testing"

// A device names a room peer by the product it is, so the peer must resolve
// whether its profile is the family (one package, several models) or the
// product itself. Regression for the family rename (#2826): a video bar's
// {room-device:newtron-panel-7} resolved empty once the panel's profile
// became the newtron-panel family.
func TestRoomPeersResolveByProductAndProfile(t *testing.T) {
	snaps := []DeviceSnapshot{
		{Hostname: "hq-1-touch-control-1", ProfileName: "newtron-panel", Product: "newtron-panel-7", SiteCode: "hq", Room: "1"},
		{Hostname: "hq-1-video-bar", ProfileName: "kestrel-vroom", SiteCode: "hq", Room: "1"},
		{Hostname: "hq-2-touch-control-1", ProfileName: "newtron-panel", Product: "newtron-panel-10", SiteCode: "hq", Room: "2"},
	}
	peers := roomPeers(snaps)[roomKey{"hq", "", "1"}]
	if got := peers["newtron-panel-7"]; got != "hq-1-touch-control-1" {
		t.Errorf("by product: got %q, want hq-1-touch-control-1", got)
	}
	if got := peers["newtron-panel"]; got != "hq-1-touch-control-1" {
		t.Errorf("by profile: got %q, want hq-1-touch-control-1", got)
	}
	if got := peers["kestrel-vroom"]; got != "hq-1-video-bar" {
		t.Errorf("profile without product: got %q, want hq-1-video-bar", got)
	}
	if _, leaked := peers["newtron-panel-10"]; leaked {
		t.Error("a peer from another room leaked in")
	}
}
