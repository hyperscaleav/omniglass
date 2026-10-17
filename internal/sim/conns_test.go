package sim

import (
	"io"
	"net"
	"testing"
	"time"
)

// A tracked listener's Close ends the connections it accepted, not just the
// accept loop, so a stopped device stops answering clients mid-session.
func TestTrackedListenerCloseEndsLiveConns(t *testing.T) {
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	lis := trackConns(raw)
	go func() {
		for {
			c, err := lis.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(io.Discard, c) }() // hold it open
		}
	}()
	client, err := net.Dial("tcp", raw.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	time.Sleep(50 * time.Millisecond) // let Accept register it

	if err := lis.Close(); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := client.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("read after close = %v, want EOF", err)
	}
}

// A device's SSH and telnet stops end established connections.
func TestStopEndsEstablishedStreamConns(t *testing.T) {
	for _, tc := range []struct {
		name  string
		start func(d *Device) error
		stop  func(d *Device)
		addr  func(d *Device) net.Addr
	}{
		{"ssh", func(d *Device) error { return d.StartSSH(0, "admin", "admin", false) }, (*Device).StopSSH,
			func(d *Device) net.Addr { return d.sshListener.Addr() }},
		{"telnet", func(d *Device) error { return d.StartTelnet(0, "admin", "admin", false) }, (*Device).StopTelnet,
			func(d *Device) net.Addr { return d.telnetListener.Addr() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := NewDevice("d", "h", net.ParseIP("127.0.0.1"), "p", 1, map[string]any{})
			if err := tc.start(d); err != nil {
				t.Fatal(err)
			}
			c, err := net.Dial("tcp", tc.addr(d).String())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			time.Sleep(50 * time.Millisecond)
			tc.stop(d)
			_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
			buf := make([]byte, 4096)
			for {
				if _, err := c.Read(buf); err != nil {
					if ne, ok := err.(net.Error); ok && ne.Timeout() {
						t.Fatal("connection still open after stop")
					}
					return
				}
			}
		})
	}
}
