package collection_test

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// Telnet option negotiation (#1033): the client refuses every option. Inbound
// IAC sequences are stripped from the stream (including ones split across
// reads), DO is answered WONT and WILL is answered DONT, and a literal 0xFF in
// what the session writes is escaped.

const (
	iac  = 0xFF
	will = 0xFB
	wont = 0xFC
	do   = 0xFD
	dont = 0xFE
	sb   = 0xFA
	se   = 0xF0
)

func telnetPair(t *testing.T) (client net.Conn, dev net.Conn) {
	t.Helper()
	c, d := net.Pipe()
	t.Cleanup(func() { _ = c.Close(); _ = d.Close() })
	return collection.NewTelnetConn(c), d
}

// readAll reads from the telnet side until n data bytes arrived.
func readN(t *testing.T, c net.Conn, n int) []byte {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	out := make([]byte, 0, n)
	buf := make([]byte, 64)
	for len(out) < n {
		k, err := c.Read(buf)
		out = append(out, buf[:k]...)
		if err != nil {
			t.Fatalf("read after %q: %v", out, err)
		}
	}
	return out
}

func TestTelnetStripsNegotiationAndRefusesEveryOption(t *testing.T) {
	client, dev := telnetPair(t)
	replies := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 64)
		_ = dev.SetReadDeadline(time.Now().Add(2 * time.Second))
		var got []byte
		for len(got) < 6 {
			n, err := dev.Read(buf)
			got = append(got, buf[:n]...)
			if err != nil {
				break
			}
		}
		replies <- got
	}()
	go func() {
		_, _ = dev.Write([]byte{iac, will, 0x01, iac, do, 0x18})
		_, _ = dev.Write([]byte("login: "))
	}()
	if got := readN(t, client, 7); string(got) != "login: " {
		t.Fatalf("data = %q, want the prompt with every IAC sequence stripped", got)
	}
	want := []byte{iac, dont, 0x01, iac, wont, 0x18}
	if got := <-replies; !bytes.Equal(got, want) {
		t.Fatalf("negotiation replies = % x, want % x", got, want)
	}
}

func TestTelnetStripsSequencesSplitAcrossReads(t *testing.T) {
	client, dev := telnetPair(t)
	go func() { _, _ = io.Copy(io.Discard, dev) }()
	go func() {
		// IAC, then its verb and option on later writes; a subnegotiation;
		// an escaped 0xFF data byte.
		_, _ = dev.Write([]byte{'a', iac})
		_, _ = dev.Write([]byte{dont})
		_, _ = dev.Write([]byte{0x01, 'b', iac, sb, 0x18, 0x01, iac, se, 'c', iac, iac, 'd'})
	}()
	if got := readN(t, client, 5); string(got) != "abc\xffd" {
		t.Fatalf("data = %q, want abc, a literal 0xff, d", got)
	}
}

func TestTelnetEscapesALiteralFF(t *testing.T) {
	client, dev := telnetPair(t)
	got := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 16)
		n, _ := io.ReadAtLeast(dev, buf, 4)
		got <- buf[:n]
	}()
	if _, err := client.Write([]byte{'x', iac, 'y'}); err != nil {
		t.Fatal(err)
	}
	if b := <-got; !bytes.Equal(b, []byte{'x', iac, iac, 'y'}) {
		t.Fatalf("wire = % x, want the 0xff doubled", b)
	}
}
