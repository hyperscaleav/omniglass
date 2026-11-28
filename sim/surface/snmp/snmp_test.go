package snmp

import "testing"

func TestSystemScalars(t *testing.T) {
	oids := System{Descr: func() string { return "X" }, UptimeSeconds: func() int64 { return 5 }}.OIDs()
	if len(oids) != 2 || oids[0].OID != OIDSysDescr || oids[0].Get() != "X" {
		t.Fatalf("got %+v", oids)
	}
	if oids[1].OID != OIDSysUpTime || oids[1].Get() != uint32(500) {
		t.Fatalf("uptime is hundredths of a second: %+v -> %v", oids[1], oids[1].Get())
	}
}

func TestIdentityArcAndOmittedScalars(t *testing.T) {
	both := Identity{Enterprise: 55530, Firmware: func() string { return "1" }, Serial: func() string { return "2" }}.OIDs()
	if len(both) != 2 || both[0].OID != "1.3.6.1.4.1.55530.5.1.0" || both[1].OID != "1.3.6.1.4.1.55530.5.2.0" {
		t.Fatalf("got %+v", both)
	}
	only := Identity{Enterprise: 55520, Serial: func() string { return "2" }}.OIDs()
	if len(only) != 1 || only[0].OID != "1.3.6.1.4.1.55520.5.2.0" {
		t.Fatalf("got %+v", only)
	}
}
