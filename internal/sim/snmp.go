package sim

import (
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/slayercat/GoSNMPServer"
)

// StartSNMP binds an SNMPv2c listener on <LoopbackIP>:port. Handles GET
// and GETNEXT against the device's OIDMap; WALK is a client-driven loop
// of GETNEXT calls so we do not need a separate path. Accepts the given
// community plus "public" (convention for demo posture).
func (d *Device) StartSNMP(port int, community string) error {
	addr := fmt.Sprintf("%s:%d", d.LoopbackIP.String(), port)

	// Build a sorted snapshot of OIDs so GETNEXT can find successors in
	// numeric lexicographic order.
	oids := d.sortedOIDs()

	handler := GoSNMPServer.MasterAgent{
		Logger: GoSNMPServer.NewDiscardLogger(), // per-request debug noise; hits report through RequestPublisher
		SecurityConfig: GoSNMPServer.SecurityConfig{
			NoSecurity: true,
		},
		SubAgents: []*GoSNMPServer.SubAgent{
			{
				CommunityIDs: []string{community, "public"},
				OIDs:         d.buildOIDEntries(oids, port, community),
			},
		},
	}

	srv := GoSNMPServer.NewSNMPServer(handler)
	if err := srv.ListenUDP("udp", addr); err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}

	go func() {
		slog.Info("device SNMP started", "device", d.Hostname, "addr", addr)
		if err := srv.ServeForever(); err != nil && !strings.Contains(err.Error(), "closed") {
			slog.Error("device SNMP error", "device", d.Hostname, "error", err)
		}
	}()
	// GoSNMPServer does not expose its underlying PacketConn for us to
	// stash on d.snmpConn, so StopSNMP calls its own Shutdown method via
	// a captured reference on the device.
	d.snmpServer = srv
	return nil
}

// StopSNMP shuts down the SNMP listener.
func (d *Device) StopSNMP() {
	if d.snmpServer != nil {
		d.snmpServer.Shutdown()
	}
}

// sortedOIDs returns the OIDs the device answers, in numeric
// lexicographic order. Used so GETNEXT can walk them deterministically.
func (d *Device) sortedOIDs() []string {
	out := make([]string, 0, len(d.OIDMap))
	for oid := range d.OIDMap {
		out = append(out, oid)
	}
	sort.Slice(out, func(i, j int) bool {
		return oidLess(out[i], out[j])
	})
	return out
}

func oidLess(a, b string) bool {
	as := strings.Split(strings.TrimPrefix(a, "."), ".")
	bs := strings.Split(strings.TrimPrefix(b, "."), ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		ai, _ := strconv.Atoi(as[i])
		bi, _ := strconv.Atoi(bs[i])
		if ai != bi {
			return ai < bi
		}
	}
	return len(as) < len(bs)
}

// buildOIDEntries turns the device's OID map into GoSNMPServer entries.
// Each entry dereferences the device's live state when queried so values
// reflect ongoing behavior changes. port and community are captured per
// entry so the OnGet hook can publish a RequestEvent with full forensic
// scope, operators searching the global log for an OID, a community
// string, or a specific port number need that context surfaced.
//
// GoSNMPServer's API hands us per-OID OnGet callbacks rather than per-
// packet. So one inbound SNMP packet with N varbinds produces N events.
// At typical collector scale (one OID per item, ~minute cadence) this is
// sub-Hz per device, well within the SSE stream's headroom. SNMP
// bulkwalk would amplify (50 OIDs per packet → 50 events) but no
// current consumer issues bulkwalk; revisit if that changes.
func (d *Device) buildOIDEntries(sorted []string, port int, community string) []*GoSNMPServer.PDUValueControlItem {
	// Pre-compute the listener bind address once so the per-OID
	// closure doesn't re-allocate on every probe. Carried as the
	// event's LocalAddr since gosnmp's OnGet doesn't expose the source.
	listenerAddr := fmt.Sprintf("%s:%d", d.LoopbackIP.String(), port)
	items := make([]*GoSNMPServer.PDUValueControlItem, 0, len(sorted))
	for _, oid := range sorted {
		oid := oid
		statePath := d.OIDMap[oid]
		asn1Type := snmpTypeFor(statePath)
		items = append(items, &GoSNMPServer.PDUValueControlItem{
			OID:  oid,
			Type: asn1Type,
			OnGet: func() (interface{}, error) {
				start := time.Now()
				val, err := d.snmpValue(oid, statePath, asn1Type)
				if d.RequestPublisher != nil {
					status := 200
					if err != nil {
						status = 500
					}
					// Method="GET" is conservative, gosnmp routes both
					// GET and GETNEXT through OnGet (GETNEXT is a successor
					// lookup that resolves to a specific OID, which is
					// what reaches us). Distinguishing here would require
					// a deeper hook into the SNMP server pipeline that
					// the library doesn't expose.
					//
					// LocalAddr carries the raw listener address because
					// gosnmp doesn't expose the peer through OnGet; source
					// labelling is left to the hit consumer.
					d.RequestPublisher(RequestEvent{
						DeviceID:   d.DeviceID,
						Hostname:   d.Hostname,
						Protocol:   "snmp",
						Method:     "GET",
						Port:       port,
						Path:       oid,
						Status:     status,
						LocalAddr:  listenerAddr,
						DurationMs: time.Since(start).Milliseconds(),
						Details: map[string]any{
							"oid":        oid,
							"value":      val,
							"asn1_type":  asn1Type.String(),
							"state_path": statePath,
							"community":  community,
						},
					})
				}
				return val, err
			},
		})
	}
	return items
}

// snmpValue resolves the device's current state at statePath and returns
// the typed value gosnmp expects for the declared OID Type. Return type
// must exactly match asn1Type, the SNMP server will panic (and time
// the client out) on a mismatch. Wrap helpers (Asn1OctetStringWrap etc.)
// document the expected Go types per ASN.1 BER.
func (d *Device) snmpValue(oid, statePath string, asn1Type gosnmp.Asn1BER) (interface{}, error) {
	// ReadPath resolves computed fields (e.g. sysUpTime as elapsed-since-
	// boot) ahead of stored state, taking d.mu internally.
	v := d.ReadPath(statePath)

	// sysUpTime is always reported as hundredths of a second.
	if oid == "1.3.6.1.2.1.1.3.0" {
		return uint32(toInt64(v) * 100), nil
	}

	switch asn1Type {
	case gosnmp.Integer:
		return int(toInt64(v)), nil
	case gosnmp.Counter32, gosnmp.Gauge32:
		return uint(toInt64(v)), nil
	case gosnmp.Counter64:
		return uint64(toInt64(v)), nil
	case gosnmp.TimeTicks:
		return uint32(toInt64(v)), nil
	case gosnmp.OctetString:
		return toString(v), nil
	default:
		return toString(v), nil
	}
}

func toString(v any) string {
	switch vv := v.(type) {
	case nil:
		return ""
	case string:
		return vv
	case bool:
		if vv {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", vv)
	}
}

// snmpTypeFor picks the gosnmp ASN.1 type to declare for an OID. Numeric
// state paths get Integer; strings get OctetString. We can't reliably
// introspect the type without a sample so we bias by path name
// convention (uptime -> TimeTicks, *_bytes -> Counter32, others
// OctetString because profiles often mix string/int at the same path).
func snmpTypeFor(statePath string) gosnmp.Asn1BER {
	switch {
	case strings.Contains(statePath, "uptime"):
		return gosnmp.TimeTicks
	case strings.HasSuffix(statePath, "_bytes"), strings.HasSuffix(statePath, "_rpm"):
		return gosnmp.Counter32
	case strings.Contains(statePath, "brightness"),
		strings.Contains(statePath, "volume"),
		strings.Contains(statePath, "temperature"),
		strings.Contains(statePath, "cpu_load"),
		strings.Contains(statePath, "active_channels"),
		strings.Contains(statePath, "participants"),
		strings.Contains(statePath, "fan"):
		return gosnmp.Integer
	default:
		return gosnmp.OctetString
	}
}

// lookupStatePath walks a dot-separated path into a nested state tree.
// Map keys index map[string]any segments; numeric segments index into
// []any slices. Returns nil for any missing or out-of-range segment.
func lookupStatePath(root map[string]any, path string) any {
	if root == nil || path == "" {
		return nil
	}
	cur := any(root)
	for _, seg := range strings.Split(path, ".") {
		switch v := cur.(type) {
		case map[string]any:
			cur = v[seg]
		case []any:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(v) {
				return nil
			}
			cur = v[idx]
		default:
			return nil
		}
		if cur == nil {
			return nil
		}
	}
	return cur
}

func toInt64(v any) int64 {
	switch vv := v.(type) {
	case int:
		return int64(vv)
	case int64:
		return vv
	case float64:
		return int64(vv)
	case bool:
		if vv {
			return 1
		}
		return 0
	default:
		return 0
	}
}
