package collection

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/gosnmp/gosnmp"
)

// The SNMP transport client (#814): a v2c scalar GET, the generic driver's
// whole request shape (walk and discovery are out of the epic's scope). The
// getter is a capability wrapper like the pingers and probers: an interface
// the runner consumes, a real implementation over the wire, and the real
// implementation proven by an integration test against an actual agent.

// SNMPGetter runs one v2c scalar GET and returns the response values keyed by
// OID, every value rendered as a string (the interpreter types them by lane).
type SNMPGetter interface {
	Get(ctx context.Context, target, community string, oids []string, timeout time.Duration) (map[string]string, error)
}

// NewSNMPGetter returns the real gosnmp-backed getter.
func NewSNMPGetter() SNMPGetter { return &snmpGetter{} }

type snmpGetter struct{}

func (g *snmpGetter) Get(ctx context.Context, target, community string, oids []string, timeout time.Duration) (map[string]string, error) {
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		// A bare host polls the well-known port, mirroring how the probes
		// treat a portless target.
		host, portStr = target, "161"
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("collection: snmp target %q: bad port: %w", target, err)
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	client := &gosnmp.GoSNMP{
		Target:    host,
		Port:      uint16(port),
		Community: community,
		Version:   gosnmp.Version2c,
		Timeout:   timeout,
		Retries:   0,
		Context:   ctx,
	}
	if err := client.Connect(); err != nil {
		return nil, fmt.Errorf("collection: snmp connect %s: %w", target, err)
	}
	defer func() { _ = client.Conn.Close() }()

	res, err := client.Get(oids)
	if err != nil {
		return nil, fmt.Errorf("collection: snmp get %s: %w", target, err)
	}
	out := make(map[string]string, len(res.Variables))
	for _, v := range res.Variables {
		name := v.Name
		if len(name) > 0 && name[0] == '.' {
			name = name[1:]
		}
		s, ok := renderSNMPValue(v)
		if !ok {
			// noSuchObject / noSuchInstance / a type the menu cannot carry:
			// leave it out, and the interpreter's locate step faults it by
			// name rather than shipping a placeholder.
			continue
		}
		out[name] = s
	}
	return out, nil
}

// renderSNMPValue stringifies one varbind: octet strings as text, the numeric
// families in decimal. ok is false for the exception markers and types the
// interpreter has no lane story for.
func renderSNMPValue(v gosnmp.SnmpPDU) (string, bool) {
	switch v.Type {
	case gosnmp.OctetString:
		b, ok := v.Value.([]byte)
		if !ok {
			return "", false
		}
		return string(b), true
	case gosnmp.Integer, gosnmp.Counter32, gosnmp.Gauge32, gosnmp.TimeTicks, gosnmp.Counter64, gosnmp.Uinteger32:
		return fmt.Sprintf("%d", gosnmp.ToBigInt(v.Value)), true
	case gosnmp.ObjectIdentifier:
		s, ok := v.Value.(string)
		return s, ok
	case gosnmp.IPAddress:
		s, ok := v.Value.(string)
		return s, ok
	default:
		return "", false
	}
}
