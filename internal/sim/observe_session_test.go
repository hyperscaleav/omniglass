package sim

import (
	"bufio"
	"net"
	"testing"
	"time"
)

// telnetSession logs in and returns the open connection and its reader,
// positioned after the first prompt (or the rejection).
func telnetSession(t *testing.T, addr, pass string) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	rd := bufio.NewReader(telnetReader{r: conn})
	for _, step := range []struct{ prompt, send string }{{"login:", "admin"}, {"Password:", pass}} {
		if err := readUntil(rd, step.prompt); err != nil {
			t.Fatalf("waiting for %q: %v", step.prompt, err)
		}
		if _, err := conn.Write([]byte(step.send + "\r\n")); err != nil {
			t.Fatal(err)
		}
	}
	return conn, rd
}

func TestObservedTelnetLinesAndRefusal(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket test")
	}
	dev := NewDevice("d", "tel-host", net.ParseIP("127.0.0.1"), "p", 1, map[string]any{})
	if err := dev.AddCommand("show version", "v.tmpl", "1.0\n", nil); err != nil {
		t.Fatal(err)
	}
	if err := dev.StartTelnet(0, "admin", "polaris", false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dev.StopTelnet)
	addr := dev.telnetListener.Addr().String()

	conn, rd := telnetSession(t, addr, "polaris")
	if err := readUntil(rd, "tel-host>"); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"show version", "no such command"} {
		if _, err := conn.Write([]byte(line + "\r\n")); err != nil {
			t.Fatal(err)
		}
		if err := readUntil(rd, "tel-host>"); err != nil {
			t.Fatal(err)
		}
	}
	c := observedEventually(t, dev, "telnet", func(c ObservationCounters) bool { return c.Settled() == 2 })
	if c.Accepted != 1 || c.OpenConnections != 1 || c.Answered != 1 || c.Error != 1 {
		t.Fatalf("an answered and an unknown line = %+v", c)
	}
	_ = conn.Close()

	_, rd = telnetSession(t, addr, "wrong")
	if err := readUntil(rd, "auth rejected"); err != nil {
		t.Fatal(err)
	}
	c = observedEventually(t, dev, "telnet", func(c ObservationCounters) bool { return c.Settled() == 3 && c.OpenConnections == 0 })
	if c.Accepted != 2 || c.Refused != 1 || c.Received != 3 {
		t.Fatalf("after a wrong password = %+v, want one refused", c)
	}
}

func TestObservedTCPSessionLines(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket test")
	}
	dev, _ := newQRCDevice(t, "qrc-observed")
	if err := dev.StartTCPSession(0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dev.StopTCPSession)
	conn, err := net.Dial("tcp", dev.TCPSessionAddrs()[0].String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	rd := bufio.NewReader(conn)
	if _, err := rd.ReadBytes(0); err != nil { // the welcome is not a request
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("set channels.0.gain -9\x00")); err != nil {
		t.Fatal(err)
	}
	if _, err := rd.ReadBytes(0); err != nil {
		t.Fatal(err)
	}
	c := observedEventually(t, dev, "tcp", func(c ObservationCounters) bool { return c.Settled() == 1 })
	if c.Accepted != 1 || c.OpenConnections != 1 || c.Received != 1 || c.Answered != 1 {
		t.Fatalf("one answered line = %+v", c)
	}
	dev.StopTCPSession()
	c = observedEventually(t, dev, "tcp", func(c ObservationCounters) bool { return c.OpenConnections == 0 })
	if c.Accepted != 1 {
		t.Fatalf("after stop: %+v", c)
	}
}
