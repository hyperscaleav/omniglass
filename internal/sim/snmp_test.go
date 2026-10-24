package sim

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
)

func TestSNMPGet_ReturnsDeclaredOID(t *testing.T) {
	dev := NewDevice("dev-1", "test-host", net.ParseIP("127.0.0.1"), "test-profile", 1,
		map[string]any{
			"status": map[string]any{
				"device": map[string]any{"power": "on"},
				"audio":  map[string]any{"volume": 42},
			},
		})
	dev.OIDMap = map[string]string{
		"1.3.6.1.4.1.99999.1.1.0": "status.device.power", // string
		"1.3.6.1.4.1.99999.2.1.0": "status.audio.volume", // int (Integer per snmpTypeFor)
	}

	// Port 0: the kernel picks a free port, and the agent reports it.
	bound, err := dev.startSNMP(0, "public")
	if err != nil {
		t.Fatalf("StartSNMP: %v", err)
	}
	port := boundPort(bound)
	t.Cleanup(dev.StopSNMP)

	// Give the listener a moment to start accepting.
	time.Sleep(50 * time.Millisecond)

	t.Run("string OID", func(t *testing.T) {
		val := snmpGet(t, "127.0.0.1", uint16(port), "1.3.6.1.4.1.99999.1.1.0")
		if s, ok := val.([]byte); ok {
			if string(s) != "on" {
				t.Errorf("power = %q, want on", s)
			}
		} else if s, ok := val.(string); ok {
			if s != "on" {
				t.Errorf("power = %q, want on", s)
			}
		} else {
			t.Errorf("unexpected type %T for power OID: %v", val, val)
		}
	})

	t.Run("integer OID", func(t *testing.T) {
		val := snmpGet(t, "127.0.0.1", uint16(port), "1.3.6.1.4.1.99999.2.1.0")
		if i, ok := val.(int); ok {
			if i != 42 {
				t.Errorf("volume = %d, want 42", i)
			}
		} else {
			t.Errorf("unexpected type %T for volume OID: %v", val, val)
		}
	})
}

// TestSNMPGet_PublishesRequestEvent pins the new RequestPublisher hook:
// every OID GET fires one RequestEvent with forensic detail (oid, value,
// asn1 type, state path, community). This is what feeds the console's
// click-to-expand log entry, without this, SNMP probes would have no
// payload visibility.
func TestSNMPGet_PublishesRequestEvent(t *testing.T) {
	dev := NewDevice("dev-1", "test-host", net.ParseIP("127.0.0.1"), "test-profile", 1,
		map[string]any{
			"status": map[string]any{
				"device": map[string]any{"power": "on"},
			},
		})
	dev.OIDMap = map[string]string{
		"1.3.6.1.4.1.99999.1.1.0": "status.device.power",
	}

	var (
		mu       sync.Mutex
		captured []RequestEvent
	)
	dev.RequestPublisher = func(re RequestEvent) {
		mu.Lock()
		captured = append(captured, re)
		mu.Unlock()
	}

	bound, err := dev.startSNMP(0, "private-community")
	if err != nil {
		t.Fatalf("StartSNMP: %v", err)
	}
	port := boundPort(bound)
	t.Cleanup(dev.StopSNMP)
	time.Sleep(50 * time.Millisecond)

	_ = snmpGetAs(t, "127.0.0.1", uint16(port), "private-community", "1.3.6.1.4.1.99999.1.1.0")

	// gosnmp client returns synchronously, but the OnGet path invokes
	// the publisher inline, capture should be visible immediately.
	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 1 {
		t.Fatalf("captured %d events, want 1", len(captured))
	}
	got := captured[0]
	if got.DeviceID != "dev-1" || got.Hostname != "test-host" {
		t.Errorf("scope = %s/%s, want dev-1/test-host", got.DeviceID, got.Hostname)
	}
	if got.Protocol != "snmp" || got.Method != "GET" {
		t.Errorf("protocol/method = %s/%s, want snmp/GET", got.Protocol, got.Method)
	}
	if got.Port != port {
		t.Errorf("port = %d, want %d", got.Port, port)
	}
	if got.Path != "1.3.6.1.4.1.99999.1.1.0" {
		t.Errorf("path = %q, want OID", got.Path)
	}
	if got.Status != 200 {
		t.Errorf("status = %d, want 200", got.Status)
	}
	if got.Details["oid"] != "1.3.6.1.4.1.99999.1.1.0" {
		t.Errorf("details.oid = %v, want OID", got.Details["oid"])
	}
	if got.Details["state_path"] != "status.device.power" {
		t.Errorf("details.state_path = %v, want status.device.power", got.Details["state_path"])
	}
	if got.Details["community"] != "private-community" {
		t.Errorf("details.community = %v, want private-community", got.Details["community"])
	}
	if got.Details["asn1_type"] != "OctetString" {
		t.Errorf("details.asn1_type = %v, want OctetString (string OID)", got.Details["asn1_type"])
	}
}

func snmpGet(t *testing.T, host string, port uint16, oid string) any {
	t.Helper()
	return snmpGetAs(t, host, port, "public", oid)
}

// snmpGetAs gets one OID with a community: a face answers its own alone.
func snmpGetAs(t *testing.T, host string, port uint16, community, oid string) any {
	t.Helper()
	g := &gosnmp.GoSNMP{
		Target:    host,
		Port:      port,
		Community: community,
		Version:   gosnmp.Version2c,
		Timeout:   2 * time.Second,
		Retries:   1,
	}
	if err := g.Connect(); err != nil {
		t.Fatalf("snmp connect: %v", err)
	}
	defer g.Conn.Close()

	resp, err := g.Get([]string{oid})
	if err != nil {
		t.Fatalf("snmp get %s: %v", oid, err)
	}
	if len(resp.Variables) != 1 {
		t.Fatalf("expected 1 varbind, got %d", len(resp.Variables))
	}
	return resp.Variables[0].Value
}
