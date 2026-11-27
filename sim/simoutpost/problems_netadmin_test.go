//go:build linux && netadmin

package simoutpost

import (
	"net"
	"os"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/impair"
)

// #2769, the NET_ADMIN lane: a scripted packet-loss problem, applied by the
// executor through the real impairer and the kernel's own netlink, cuts the
// device's address for its window and only its window. Run through
// make test-netadmin: it rewrites the network it runs in.
func TestNetlinkScriptedLossAppliesForItsWindowAndClearsAfter(t *testing.T) {
	if os.Getenv("OMNIGLASS_NETADMIN") != "1" {
		t.Fatal("this test rewrites the network it runs in; run it through make test-netadmin")
	}
	rigLoss = 100
	t.Cleanup(func() { rigLoss = 30 })
	r := newProblemRig(t)
	const mic = "127.70.0.2" // the rig's second device, the wireless microphone
	l, err := net.Listen("tcp", mic+":8080")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	ap := impair.NewNetlink()
	m := newImpairer(ap, true)
	t.Cleanup(func() { _ = ap.Clear() })
	r.e.Arbiter().SetNet(m)
	settle := func(at time.Duration) {
		r.at(true, rigOpen.Add(at))
	}
	reachable := func() bool {
		c, err := net.DialTimeout("tcp", mic+":8080", 700*time.Millisecond)
		if err == nil {
			c.Close()
		}
		return err == nil
	}

	settle(30 * time.Minute) // before the microphone's window
	if !reachable() {
		t.Fatal("the microphone is cut before its problem opens")
	}
	settle(70 * time.Minute) // inside it
	if st := m.status(); st.Error != "" || len(st.Impaired) == 0 {
		t.Fatalf("the executor's loss did not reach the impairer: %+v", st)
	}
	if reachable() {
		t.Error("the microphone answers inside its packet-loss window")
	}
	settle(121 * time.Minute) // after it
	if !reachable() {
		t.Error("the microphone is still cut after its window closed")
	}
}
