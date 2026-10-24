package driver_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// snmp's transport rung is a standard get answered (#821): a driver that
// declares no request gets sysUpTime.0, so snmp-generic gains its rung with
// no spec change, and a declared get replaces it.

func snmpSpec() *driver.Spec {
	s := validSpec()
	s.Transport = "snmp"
	s.Polls[0].Request, s.Polls[0].Emits[0].Extract = driver.Request{Get: []string{"1.3"}}, driver.Extract{OID: "1.3"}
	s.Listeners, s.Commands = nil, nil
	return s
}

func TestAnSNMPLadderDefaultsToASysUpTimeGet(t *testing.T) {
	l := driver.ResolveLadder("snmp", nil, nil)
	if l.Transport != driver.RungRequest || l.Request == nil || len(l.Request.Get) != 1 || l.Request.Get[0] != driver.SysUpTimeOID {
		t.Fatalf("snmp's default ladder = %+v, want a request rung getting sysUpTime.0", l)
	}
	if err := snmpSpec().Validate(catalog()); err != nil {
		t.Fatalf("an snmp driver declaring no ladder refused: %v", err)
	}
	declared := &driver.Ladder{Transport: &driver.TransportRung{Request: &driver.Request{Get: []string{"1.3.6.1.2.1.1.2.0"}}}}
	if got := driver.ResolveLadder("snmp", declared, nil); len(got.Request.Get) != 1 || got.Request.Get[0] != "1.3.6.1.2.1.1.2.0" {
		t.Fatalf("a declared get = %+v, want it to replace sysUpTime", got.Request)
	}
	s := snmpSpec()
	s.Ladder = declared
	if err := s.Validate(catalog()); err != nil {
		t.Fatalf("an snmp driver declaring sysObjectID refused: %v", err)
	}
	// Another transport's default ladder carries no request.
	if l := driver.ResolveLadder("udp", nil, nil); l.Request != nil {
		t.Fatalf("udp's default ladder = %+v, want no request (its driver names one)", l)
	}
}
