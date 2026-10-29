//go:build linux

package sim

import (
	"testing"

	"golang.org/x/crypto/ssh"
)

// A simulated device's ssh host key is derived from its id (#1370), as its
// https key is: a node that pinned it on first use finds the same key after
// the simulator restarts, so a healthy ssh endpoint in a kept lab stays up.
// Another device presents another key, and a rotation (a reimaged device)
// still moves it.
func TestSSHHostKeyIsTheSameAcrossASimulatorRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("binds an ssh listener")
	}
	key := func(id, ip string) string {
		t.Helper()
		e := newCatalogEngine(t)
		if err := e.Sync([]DeviceSnapshot{{DeviceID: id, Hostname: id, LoopbackIP: ip, ProfileName: "newtron-panel-7", Version: 1}}); err != nil {
			t.Fatal(err)
		}
		d := e.Devices()[0]
		if d.SSHHostKey() == nil {
			t.Fatal("the panel serves no ssh host key")
		}
		fp := ssh.FingerprintSHA256(d.SSHHostKey())
		e.StopAll()
		return fp
	}
	first := key("panel-1", "127.88.9.21")
	if again := key("panel-1", "127.88.9.21"); again != first {
		t.Fatalf("the restarted panel presents %s, want the key it had, %s", again, first)
	}
	if other := key("panel-2", "127.88.9.22"); other == first {
		t.Fatalf("two panels present the same key %s", first)
	}

	e := newCatalogEngine(t)
	if err := e.Sync([]DeviceSnapshot{{DeviceID: "panel-1", Hostname: "panel-1", LoopbackIP: "127.88.9.21", ProfileName: "newtron-panel-7", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	d := e.Devices()[0]
	if err := d.RotateSSHHostKey(); err != nil {
		t.Fatal(err)
	}
	if rotated := ssh.FingerprintSHA256(d.SSHHostKey()); rotated == first {
		t.Fatal("a rotated (reimaged) panel kept its key")
	}
}
