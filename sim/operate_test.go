package sim

import (
	"net"
	"strings"
	"testing"
)

func operateDevice(t *testing.T) (*Device, *[]RequestEvent) {
	t.Helper()
	dev := NewDevice("c1", "host", net.ParseIP("127.60.0.1"), "test", 1, map[string]any{
		"power":  "off",
		"sys":    map[string]any{"name": "lobby-display"},
		"volume": 30,
	})
	if err := dev.AddRoute("GET /api/power", "p.gotempl", `{"power":"{{ .State | get "power" }}"}`, nil); err != nil {
		t.Fatal(err)
	}
	if err := dev.AddCommand("power on", "on.gotempl", `OK {{ .State | get "power" }}`, map[string]any{"power": "on"}); err != nil {
		t.Fatal(err)
	}
	dev.OIDMap = map[string]string{"1.3.6.1.2.1.1.5.0": "sys.name", "1.3.6.1.4.1.9.1.0": "volume"}
	var events []RequestEvent
	dev.RequestPublisher = func(ev RequestEvent) { events = append(events, ev) }
	return dev, &events
}

// An HTTP operation answers from the device's own routes, as a client would
// see it, and counts as a request to the device.
func TestOperateHTTPAnswersFromTheRoutes(t *testing.T) {
	dev, _ := operateDevice(t)
	var logs []RequestLog
	dev.Logger = func(l RequestLog) { logs = append(logs, l) }
	res := dev.Operate(Operation{Kind: "http", Method: "GET", Path: "/api/power"})
	if res.Error != "" || res.Status != 200 || !strings.Contains(res.Body, `"power":"off"`) {
		t.Fatalf("result = %+v, want 200 with the power state", res)
	}
	if len(logs) == 0 {
		t.Fatal("an HTTP operation published no request event; the console's fire would not blink the device")
	}
}

// A route the device has no answer for reads as its 404, not an error.
func TestOperateHTTPUnknownRouteIs404(t *testing.T) {
	dev, _ := operateDevice(t)
	if res := dev.Operate(Operation{Kind: "http", Method: "GET", Path: "/nope"}); res.Status != 404 || res.Error != "" {
		t.Fatalf("result = %+v, want the device's 404", res)
	}
}

// An enforcing device's credentials are supplied: the console operates as
// an authorized client.
func TestOperateHTTPSuppliesTheDeviceCredentials(t *testing.T) {
	dev, _ := operateDevice(t)
	dev.HTTPAuth = HTTPAuth{Mode: HTTPAuthBasic, User: "admin", Password: "secret"}
	if res := dev.Operate(Operation{Kind: "http", Method: "GET", Path: "/api/power"}); res.Status != 200 {
		t.Fatalf("basic: result = %+v, want 200", res)
	}
	dev.HTTPAuth = HTTPAuth{Mode: HTTPAuthHeader, APIKey: "k1", HeaderName: "X-Token"}
	if res := dev.Operate(Operation{Kind: "http", Method: "GET", Path: "/api/power"}); res.Status != 200 {
		t.Fatalf("header: result = %+v, want 200", res)
	}
}

// A command changes the device's state and answers with the change, and is
// counted as a request on the command protocol.
func TestOperateCommandMutatesAndAnswers(t *testing.T) {
	dev, events := operateDevice(t)
	res := dev.Operate(Operation{Kind: "command", Command: "power on", Protocol: "telnet"})
	if res.Error != "" || res.Body != "OK on" {
		t.Fatalf("result = %+v, want the post-mutation answer", res)
	}
	if got := dev.ReadPath("power"); got != "on" {
		t.Fatalf("power = %v, want on", got)
	}
	if len(*events) != 1 || (*events)[0].Protocol != "telnet" || (*events)[0].Path != "power on" {
		t.Fatalf("events = %+v, want one telnet request for the command", *events)
	}
}

func TestOperateUnknownCommandSaysSo(t *testing.T) {
	dev, _ := operateDevice(t)
	if res := dev.Operate(Operation{Kind: "command", Command: "reboot"}); res.Error == "" {
		t.Fatalf("result = %+v, want an unknown-command error", res)
	}
}

// An SNMP get reads the live value at the OID's state path.
func TestOperateSNMPGetReadsLiveState(t *testing.T) {
	dev, events := operateDevice(t)
	res := dev.Operate(Operation{Kind: "snmp", OID: ".1.3.6.1.2.1.1.5.0"})
	if res.Error != "" || res.Body != "lobby-display" {
		t.Fatalf("result = %+v, want sysName", res)
	}
	if res := dev.Operate(Operation{Kind: "snmp", OID: "1.3.6.1.4.1.9.1.0"}); res.Body != "30" {
		t.Fatalf("volume = %+v, want 30", res)
	}
	if len(*events) != 2 || (*events)[0].Protocol != "snmp" {
		t.Fatalf("events = %+v, want two snmp requests", *events)
	}
	if res := dev.Operate(Operation{Kind: "snmp", OID: "1.2.3"}); res.Error == "" {
		t.Fatalf("an unknown OID answered %+v, want noSuchObject", res)
	}
}

func TestOperateRejectsAnUnknownKind(t *testing.T) {
	dev, _ := operateDevice(t)
	if res := dev.Operate(Operation{Kind: "ftp"}); res.Error == "" {
		t.Fatal("an unknown operation kind succeeded")
	}
}

// A large answer is cut, and says it was.
func TestOperateCapsTheBody(t *testing.T) {
	dev, _ := operateDevice(t)
	big := strings.Repeat("x", operateBodyCap+10)
	if err := dev.AddRoute("GET /big", "b.gotempl", big, nil); err != nil {
		t.Fatal(err)
	}
	res := dev.Operate(Operation{Kind: "http", Method: "GET", Path: "/big"})
	if len(res.Body) != operateBodyCap || !res.Truncated {
		t.Fatalf("body %d bytes truncated=%v, want %d and truncated", len(res.Body), res.Truncated, operateBodyCap)
	}
}

// A malformed request is the operator's typo, answered as an error, never a
// panic: a panic here would take the whole simulator down.
func TestOperateRefusesAMalformedRequest(t *testing.T) {
	dev, _ := operateDevice(t)
	for _, op := range []Operation{
		{Kind: "http", Method: "GET", Path: "/a b"},
		{Kind: "http", Method: "GET", Path: "/a\nb"},
		{Kind: "http", Method: "GET", Path: "/%zz"},
		{Kind: "http", Method: "GE T", Path: "/api/power"},
		{Kind: "http", Method: "G(T", Path: "/api/power"},
	} {
		if res := dev.Operate(op); res.Error == "" {
			t.Errorf("%+v answered %+v, want an error", op, res)
		}
	}
}

// A command a profile matches by its bytes (match_hex) is fired the way the
// surface lists it: "hex" and the pairs.
func TestOperateFiresAHexCommand(t *testing.T) {
	dev, _ := operateDevice(t)
	if err := dev.AddCommand("\x02S\x03", "h.gotempl", "ACK", nil); err != nil {
		t.Fatal(err)
	}
	if res := dev.Operate(Operation{Kind: "command", Command: "hex 02 53 03"}); res.Error != "" || res.Body != "ACK" {
		t.Fatalf("hex command answered %+v, want ACK", res)
	}
}
