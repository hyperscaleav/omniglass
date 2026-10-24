package driver_test

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// snmp's transport rung is a standard get answered (#821): a driver that
// declares no request gets sysUpTime.0, so snmp-generic gains its rung with
// no spec change, and a declared get replaces it.

func snmpSpec() *driver.Spec {
	s := validSpec()
	s.Transport = "snmp"
	s.Inputs = []driver.Input{
		{Name: "host", Kind: "string", Required: true},
		{Name: "port", Kind: "number", Default: "161"},
		{Name: "community", Kind: "secret", SecretType: "snmp-community", Required: true},
	}
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

// Every v2c request carries the community, and the engine reads it from the
// secret input named community, the poll and the rung alike: an snmp driver
// that declares none could never form its rung (review of #821, finding 3).
func TestAnSNMPDriverDeclaresItsCommunity(t *testing.T) {
	none := snmpSpec()
	none.Inputs = none.Inputs[:2]
	if err := none.Validate(catalog()); err == nil || !strings.Contains(err.Error(), "community") {
		t.Fatalf("an snmp driver with no community input = %v, want refused naming community", err)
	}
	other := snmpSpec()
	other.Inputs[2].Name = "ro-community"
	if err := other.Validate(catalog()); err == nil || !strings.Contains(err.Error(), "community") {
		t.Fatalf("a community input under another name = %v, want refused", err)
	}
	wrongType := snmpSpec()
	wrongType.Inputs[2].SecretType = "basic-auth"
	if err := wrongType.Validate(catalog()); err == nil || !strings.Contains(err.Error(), "snmp-community") {
		t.Fatalf("a community of another secret type = %v, want refused naming snmp-community", err)
	}
}
