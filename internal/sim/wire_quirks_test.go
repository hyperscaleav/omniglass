package sim

import (
	"bufio"
	"bytes"
	"net"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// TestSessionConfig_ReadDelim_StripInbound covers the inbound side of
// the wire-quirks plumbing: any single-byte or multi-byte line
// terminator, optional STX/ETX brackets, optional trailing checksum
// strip. Each case asserts the rendered "command payload" matches
// what runShellSession would dispatch.
func TestSessionConfig_ReadDelim_StripInbound(t *testing.T) {
	cases := []struct {
		name      string
		cfg       SessionConfig
		raw       []byte // includes trailing terminator
		wantDelim byte
		wantCmd   string
	}{
		{
			name:      "default CRLF",
			cfg:       SessionConfig{LineEnding: "\r\n"},
			raw:       []byte("hello\r\n"),
			wantDelim: '\n',
			wantCmd:   "hello",
		},
		{
			name:      "NUL terminator (Q-SYS QRC)",
			cfg:       SessionConfig{LineEnding: "\x00"},
			raw:       []byte(`{"jsonrpc":"2.0"}` + "\x00"),
			wantDelim: 0,
			wantCmd:   `{"jsonrpc":"2.0"}`,
		},
		{
			name:      "CR only (legacy gear)",
			cfg:       SessionConfig{LineEnding: "\r"},
			raw:       []byte("ka 01 00\r"),
			wantDelim: '\r',
			wantCmd:   "ka 01 00",
		},
		{
			name:      "STX/ETX brackets stripped",
			cfg:       SessionConfig{LineEnding: "\r\n", FrameStart: "\x02", FrameEnd: "\x03"},
			raw:       []byte("\x02PING\x03\r\n"),
			wantDelim: '\n',
			wantCmd:   "PING",
		},
		{
			name:      "trailing checksum byte stripped",
			cfg:       SessionConfig{LineEnding: "\r", StripChecksumBytes: 1},
			raw:       []byte("ka 01 00\x4F\r"),
			wantDelim: '\r',
			wantCmd:   "ka 01 00",
		},
		{
			name:      "checksum AND brackets together",
			cfg:       SessionConfig{LineEnding: "\r\n", FrameStart: "\x02", FrameEnd: "\x03", StripChecksumBytes: 2},
			raw:       []byte("\x02PING\x03\xAB\xCD\r\n"),
			wantDelim: '\n',
			wantCmd:   "PING",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.ReadDelim(); got != tc.wantDelim {
				t.Errorf("ReadDelim() = %q (%d), want %q (%d)", got, got, tc.wantDelim, tc.wantDelim)
			}
			stripped := tc.cfg.StripInbound(tc.raw)
			if got := string(stripped); got != tc.wantCmd {
				t.Errorf("StripInbound() = %q, want %q", got, tc.wantCmd)
			}
		})
	}
}

// TestSessionConfig_WrapOutbound asserts the outbound side: payload
// gets wrapped in FrameStart/FrameEnd and suffixed with LineEnding.
// Symmetric with the inbound StripInbound, what we wrap on send,
// the peer's reader strips on receive.
func TestSessionConfig_WrapOutbound(t *testing.T) {
	cases := []struct {
		name    string
		cfg     SessionConfig
		payload string
		want    string
	}{
		{
			name:    "default CRLF, no brackets",
			cfg:     SessionConfig{LineEnding: "\r\n"},
			payload: "+OK",
			want:    "+OK\r\n",
		},
		{
			name:    "NUL terminator",
			cfg:     SessionConfig{LineEnding: "\x00"},
			payload: `{"result":true}`,
			want:    `{"result":true}` + "\x00",
		},
		{
			name:    "STX/ETX wrap",
			cfg:     SessionConfig{LineEnding: "\r\n", FrameStart: "\x02", FrameEnd: "\x03"},
			payload: "PONG",
			want:    "\x02PONG\x03\r\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.WrapOutbound(tc.payload); got != tc.want {
				t.Errorf("WrapOutbound(%q) = %q, want %q", tc.payload, got, tc.want)
			}
		})
	}
}

// TestDecodeHexBytes covers the match_hex pre-load decoder.
// Whitespace-tolerant, case-insensitive, fails loudly on odd-length
// or non-hex input.
func TestDecodeHexBytes(t *testing.T) {
	ok := []struct {
		in   string
		want string
	}{
		{"02 53 00 03", "\x02S\x00\x03"},
		{"025300 03", "\x02S\x00\x03"},
		{"deadBEEF", "\xde\xad\xbe\xef"},
		{"", ""},
	}
	for _, c := range ok {
		got, err := decodeHexBytes(c.in)
		if err != nil {
			t.Errorf("decodeHexBytes(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("decodeHexBytes(%q) = %x, want %x", c.in, got, c.want)
		}
	}
	bad := []string{"02 5", "0Z", "not-hex"}
	for _, c := range bad {
		if _, err := decodeHexBytes(c); err == nil {
			t.Errorf("decodeHexBytes(%q): expected error, got nil", c)
		}
	}
}

// TestTemplateHelpers exercises the new wire-format helpers exposed
// to profile templates: xor_checksum, sum_byte_checksum_hex,
// ascii_hex, hex_byte. Each gets a small known-answer table.
func TestTemplateHelpers(t *testing.T) {
	tmpl, err := template.New("t").Funcs(templateFuncs).Parse(
		`{{ xor_checksum "ABC" }}|` +
			`{{ sum_byte_checksum_hex "ABC" }}|` +
			`{{ ascii_hex 15 2 }}|` +
			`{{ ascii_hex 255 4 }}|` +
			`{{ hex_byte "02" }}|` +
			`{{ hex_byte "FF" }}`,
	)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, nil); err != nil {
		t.Fatalf("exec: %v", err)
	}
	parts := strings.Split(buf.String(), "|")
	if len(parts) != 6 {
		t.Fatalf("got %d parts, want 6: %q", len(parts), buf.String())
	}
	// xor of 'A'=0x41, 'B'=0x42, 'C'=0x43 → 0x40
	if parts[0] != "\x40" {
		t.Errorf("xor_checksum: got %x, want 0x40", parts[0])
	}
	// 0x41+0x42+0x43 = 0xC6
	if parts[1] != "C6" {
		t.Errorf("sum_byte_checksum_hex: got %q, want C6", parts[1])
	}
	if parts[2] != "0F" {
		t.Errorf("ascii_hex(15,2): got %q, want 0F", parts[2])
	}
	if parts[3] != "00FF" {
		t.Errorf("ascii_hex(255,4): got %q, want 00FF", parts[3])
	}
	if parts[4] != "\x02" {
		t.Errorf("hex_byte(02): got %x, want 02", parts[4])
	}
	if parts[5] != "\xff" {
		t.Errorf("hex_byte(FF): got %x, want FF", parts[5])
	}
}

// TestRawTCPSession exercises the full raw-TCP listener path: open a
// TCP connection, send a NUL-terminated request, read a NUL-
// terminated response, subscribe, mutate state to trigger a push,
// confirm the push line is correctly NUL-framed. This is the
// integration counterpart to the SSH-based shellsession_test.
func TestRawTCPSession(t *testing.T) {
	dev, _ := newQRCDevice(t, "polaris-qrc-test")
	if err := dev.StartTCPSession(0); err != nil {
		t.Fatalf("StartTCPSession: %v", err)
	}
	t.Cleanup(dev.StopTCPSession)

	addr := dev.tcpSessionListener.Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	defer conn.Close()
	rd := bufio.NewReader(conn)

	// Welcome banner first, terminated by NUL per profile.
	welcome, err := rd.ReadBytes(0)
	if err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	if !bytes.Contains(welcome, []byte("EngineStatus")) {
		t.Errorf("welcome missing EngineStatus: %q", welcome)
	}
	if welcome[len(welcome)-1] != 0 {
		t.Errorf("welcome not NUL-terminated: %x", welcome)
	}

	// Subscribe.
	if _, err := conn.Write([]byte(`{"jsonrpc":"2.0","method":"ChangeGroup.AddControl","params":{"Id":"sub1","Controls":["Mixer1.gain"]},"id":3}` + "\x00")); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}
	resp, err := rd.ReadBytes(0)
	if err != nil {
		t.Fatalf("read sub resp: %v", err)
	}
	if !bytes.Contains(resp, []byte(`"id":3`)) {
		t.Errorf("sub response missing id=3: %q", resp)
	}

	// Trigger a state mutation, emit a push.
	if _, _, err := dev.RunCommand("set channels.0.gain -9"); err != nil {
		t.Fatalf("run mutate cmd: %v", err)
	}

	// Wait up to 2s for the push to arrive.
	push := waitForFrame(t, rd, 2*time.Second)
	if !bytes.Contains(push, []byte(`"Component":"Mixer1"`)) {
		t.Errorf("push missing Mixer1 marker: %q", push)
	}
	if !bytes.Contains(push, []byte(`"Value":-9`)) {
		t.Errorf("push missing Value=-9: %q", push)
	}
	if push[len(push)-1] != 0 {
		t.Errorf("push not NUL-terminated: %x", push)
	}
}

// newQRCDevice builds a minimal device with a Q-SYS-shaped (NUL-
// terminated JSON) raw-TCP persistent interface and the few
// commands the test needs. Mirrors newPTPDevice from the SSH-based
// test but with the wire-quirks knobs flipped on.
func newQRCDevice(t *testing.T, hostname string) (*Device, *eventCollector) {
	t.Helper()
	dev := NewDevice("qrc-"+hostname, hostname, net.ParseIP("127.0.0.1"), "polaris-dsp-16", 1,
		map[string]any{
			"status": map[string]any{
				"channels": []any{
					map[string]any{"index": 0, "name": "Mixer1", "gain": -12.5, "mute": false},
				},
			},
		})

	// One mutate command so the test can drive a state change without
	// HTTP; one subscribe + one unsubscribe; one read.
	add := func(c profile.Command, body string) {
		if c.Response == "" {
			c.Response = body
		}
		if err := dev.AddCommandFromProfile(&c, c.Response); err != nil {
			t.Fatalf("AddCommand %q: %v", c.Match, err)
		}
	}
	add(profile.Command{
		Match:  "set channels.0.gain -9",
		Mutate: map[string]any{"status.channels.0.gain": "-9"},
	}, "+OK")
	add(profile.Command{
		Match:    `{"jsonrpc":"2.0","method":"ChangeGroup.AddControl","params":{"Id":"sub1","Controls":["Mixer1.gain"]},"id":3}`,
		Action:   "subscribe",
		Subject:  "status.channels.0.gain",
		Token:    "sub1",
		Response: `{"jsonrpc":"2.0","result":true,"id":3}`,
	}, "")

	iface := &profile.Interface{
		Name:       "qrc",
		Protocol:   "tcp",
		Port:       0,
		Persistent: true,
		LineEnding: "\x00",
		Welcome:    `{"jsonrpc":"2.0","method":"EngineStatus","params":{"State":"Active"}}`,
		Subjects: []profile.Subject{
			{
				Match:        "status.channels.*.gain",
				Path:         "{{ .Subject }}",
				PublishToken: "Mixer1.gain",
				PushFormat:   `{"jsonrpc":"2.0","method":"ChangeGroup.Poll","params":{"Id":"{{ .Token }}","Changes":[{"Component":"Mixer1","Name":"gain","Value":{{ .Value }}}]}}`,
			},
		},
	}
	cfg, err := CompileSessionConfig(iface)
	if err != nil {
		t.Fatalf("CompileSessionConfig: %v", err)
	}
	collector := newEventCollector()
	reg := NewSessionRegistry(dev, cfg, collector.publish)
	dev.AttachTCPSession(reg)
	return dev, collector
}

// waitForFrame reads NUL-terminated frames with a deadline and
// returns the FIRST frame that doesn't look like the immediate
// open-response (the subscribe ack we already consumed). Used in
// the push-line assertion path.
func waitForFrame(t *testing.T, rd *bufio.Reader, timeout time.Duration) []byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		f, err := rd.ReadBytes(0)
		if err == nil && len(f) > 1 {
			return f
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no push frame received within %s", timeout)
	return nil
}
