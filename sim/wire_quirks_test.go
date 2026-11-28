package sim

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/wire"
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

	addr := dev.TCPSessionAddrs()[0].String()
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
	push := waitForFrame(t, conn, rd, 2*time.Second)
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

// qrcCommands is the two lines the raw-TCP test drives: a write and a subscribe.
type qrcCommands struct{}

const qrcSubscribe = `{"jsonrpc":"2.0","method":"ChangeGroup.AddControl","params":{"Id":"sub1","Controls":["Mixer1.gain"]},"id":3}`

func (qrcCommands) Commands() []string { return []string{"set channels.0.gain -9", qrcSubscribe} }

func (qrcCommands) Handle(line string) (surface.Reply, bool) {
	switch line {
	case "set channels.0.gain -9":
		return surface.Reply{Text: "+OK", Changes: []surface.Change{{Path: "status.channels.0.gain", Value: int64(-9)}}}, true
	case qrcSubscribe:
		return surface.Reply{Text: `{"jsonrpc":"2.0","result":true,"id":3}`, Action: surface.ActionSubscribe, Subject: "status.channels.0.gain", Token: "sub1"}, true
	}
	return surface.Reply{}, false
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

	// One write so the test can drive a state change without HTTP, and one
	// subscribe.
	dev.AttachCommands(qrcCommands{})

	iface := &wire.Interface{
		Name:       "qrc",
		Protocol:   "tcp",
		Port:       0,
		Persistent: true,
		LineEnding: "\x00",
		Welcome:    `{"jsonrpc":"2.0","method":"EngineStatus","params":{"State":"Active"}}`,
		Subjects: []wire.Subject{
			{
				Match:        "status.channels.*.gain",
				PublishToken: "Mixer1.gain",
				PushLine: func(_, token string, value any) string {
					raw, _ := json.Marshal(value)
					return fmt.Sprintf(`{"jsonrpc":"2.0","method":"ChangeGroup.Poll","params":{"Id":"%s","Changes":[{"Component":"Mixer1","Name":"gain","Value":%s}]}}`, token, raw)
				},
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
func waitForFrame(t *testing.T, conn net.Conn, rd *bufio.Reader, timeout time.Duration) []byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	// The read itself carries the deadline: without it a frame that never
	// comes blocks ReadBytes forever and the loop's clock never runs.
	_ = conn.SetReadDeadline(deadline)
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()
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
