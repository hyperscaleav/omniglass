package sim

import (
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func newObservedSSHDevice(t *testing.T) (*Device, string) {
	t.Helper()
	dev := NewDevice("d", "ssh-host", net.ParseIP("127.0.0.1"), "p", 1, map[string]any{})
	if err := dev.AddCommand("show version", "v.tmpl", "1.0\n", nil); err != nil {
		t.Fatal(err)
	}
	if err := dev.StartSSH(0, "admin", "kestrel", false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dev.StopSSH)
	return dev, dev.sshListener.Addr().String()
}

func TestObservedSSHExecAnswered(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket test")
	}
	dev, addr := newObservedSSHDevice(t)
	if _, ec, err := sshExec(addr, "admin", "kestrel", "show version"); err != nil || ec != 0 {
		t.Fatalf("exec: exit %d, %v", ec, err)
	}
	c := observedEventually(t, dev, "ssh", func(c ObservationCounters) bool { return c.Settled() == 1 && c.OpenConnections == 0 })
	if c.Accepted != 1 || c.Received != 1 || c.Answered != 1 || c.InFlight != 0 {
		t.Fatalf("one answered exec = %+v", c)
	}
}

func TestObservedSSHUnknownCommandIsAnError(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket test")
	}
	dev, addr := newObservedSSHDevice(t)
	if _, ec, err := sshExec(addr, "admin", "kestrel", "no such command"); err != nil || ec != 127 {
		t.Fatalf("exec: exit %d, %v", ec, err)
	}
	c := observedEventually(t, dev, "ssh", func(c ObservationCounters) bool { return c.Settled() == 1 })
	if c.Error != 1 || c.Answered != 0 {
		t.Fatalf("an unknown command = %+v, want one error", c)
	}
}

func TestObservedSSHWrongPasswordIsRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket test")
	}
	dev, addr := newObservedSSHDevice(t)
	cfg := &ssh.ClientConfig{
		User:            "admin",
		Auth:            []ssh.AuthMethod{ssh.Password("not-the-password")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	if client, err := ssh.Dial("tcp", addr, cfg); err == nil {
		client.Close()
		t.Fatal("a wrong password was let in")
	}
	c := observedEventually(t, dev, "ssh", func(c ObservationCounters) bool { return c.Settled() == 1 && c.OpenConnections == 0 })
	if c.Accepted != 1 || c.Received != 1 || c.Refused != 1 {
		t.Fatalf("a wrong password = %+v, want one refused request", c)
	}
}

// A connection the client holds open is open until the device stops, which
// ends it and lowers the gauge.
func TestObservedSSHStopEndsTheOpenConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket test")
	}
	dev, addr := newObservedSSHDevice(t)
	cfg := &ssh.ClientConfig{
		User:            "admin",
		Auth:            []ssh.AuthMethod{ssh.Password("kestrel")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	observedEventually(t, dev, "ssh", func(c ObservationCounters) bool { return c.OpenConnections == 1 })
	dev.StopSSH()
	c := observedEventually(t, dev, "ssh", func(c ObservationCounters) bool { return c.OpenConnections == 0 })
	if c.Accepted != 1 || c.Received != 0 {
		t.Fatalf("a held connection = %+v, want accepted and no request", c)
	}
}
