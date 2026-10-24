package sim

import (
	"fmt"
	"log/slog"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/slayercat/GoSNMPServer"
)

// StartSNMP binds an SNMPv2c listener on <LoopbackIP>:port. Handles GET
// and GETNEXT against the device's OIDMap; WALK is a client-driven loop
// of GETNEXT calls so we do not need a separate path. The community is
// strict (#821): the face answers its own alone, and any other meets the
// silence a real v2c agent gives a wrong community; the refused community,
// if one is set, is answered with authorizationError.
func (d *Device) StartSNMP(port int, community string) error {
	_, err := d.startSNMP(port, community)
	return err
}

// startSNMP binds and serves the SNMP agent and returns the address it bound.
// It binds before it builds the agent, so the requests it reports carry the
// port it bound.
func (d *Device) startSNMP(port int, community string) (net.Addr, error) {
	addr := fmt.Sprintf("%s:%d", d.LoopbackIP.String(), port)
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", addr, err)
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", addr, err)
	}
	addr = conn.LocalAddr().String()

	// Build a sorted snapshot of OIDs so GETNEXT can find successors in
	// numeric lexicographic order.
	oids := d.sortedOIDs()

	handler := GoSNMPServer.MasterAgent{
		Logger: snmpLogger{GoSNMPServer.NewDiscardLogger()},
		SecurityConfig: GoSNMPServer.SecurityConfig{
			NoSecurity: true,
		},
		SubAgents: []*GoSNMPServer.SubAgent{
			{
				CommunityIDs: []string{community},
				OIDs:         d.buildOIDEntries(oids, boundPort(conn.LocalAddr()), community),
			},
		},
	}

	if err := handler.ReadyForWork(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("snmp agent: %w", err)
	}
	// The read loop is ours, around the library's request handler: the
	// library's own listener nils its socket on Shutdown while its serve
	// loop reads it, a data race. Owning the socket, a stop only closes it.
	loop := &snmpLoop{conn: conn, community: community, refused: d.snmpRefused, requests: &d.snmpRequests}
	d.snmpServer = loop
	go func() {
		slog.Debug("device SNMP started", "device", d.Hostname, "addr", addr)
		loop.serve(&handler)
	}()
	return conn.LocalAddr(), nil
}

// snmpLoop answers SNMP datagrams on one socket through the library's
// request handler until the socket closes.
type snmpLoop struct {
	conn      *net.UDPConn
	community string
	refused   string
	requests  *atomic.Int64
}

func (l *snmpLoop) serve(agent *GoSNMPServer.MasterAgent) {
	var buf [65535]byte
	for {
		n, from, err := l.conn.ReadFromUDP(buf[:])
		if err != nil {
			return // closed by Shutdown
		}
		l.requests.Add(1)
		refusal, admit := l.adjudicate(buf[:n])
		if refusal != nil {
			_, _ = l.conn.WriteToUDP(refusal, from)
		}
		if !admit {
			continue
		}
		reply, err := agent.ResponseForBuffer(append([]byte(nil), buf[:n]...))
		if err != nil {
			slog.Debug("snmp request refused", "from", from.String(), "error", err)
		}
		if len(reply) > 0 {
			_, _ = l.conn.WriteToUDP(reply, from)
		}
	}
}

// adjudicate judges a request's community before the library sees it (the
// library, with no security configured, answers any community): the face's
// own is admitted; the refused one is answered with authorizationError
// (#821); any other is dropped silently, as a real v2c agent drops a wrong
// community. A datagram that does not decode is handed on, for the library
// to judge.
func (l *snmpLoop) adjudicate(raw []byte) (reply []byte, admit bool) {
	var g gosnmp.GoSNMP
	pkt, err := g.SnmpDecodePacket(append([]byte(nil), raw...))
	switch {
	case err != nil, pkt.Community == l.community:
		return nil, true
	case l.refused != "" && pkt.Community == l.refused:
		return l.refusal(pkt), false
	default:
		return nil, false
	}
}

// refusal renders the authorizationError answer to a request.
func (l *snmpLoop) refusal(pkt *gosnmp.SnmpPacket) []byte {
	pkt.PDUType = gosnmp.GetResponse
	pkt.Error = gosnmp.AuthorizationError
	pkt.ErrorIndex = 0
	out, err := pkt.MarshalMsg()
	if err != nil {
		slog.Debug("snmp refusal not marshalled", "error", err)
		return nil
	}
	return out
}

// SetSNMPRefusedCommunity sets the community the SNMP face answers with
// authorizationError; it takes effect when the face next starts.
func (d *Device) SetSNMPRefusedCommunity(c string) { d.snmpRefused = c }

// SNMPRequests counts the datagrams the SNMP face was sent: a test-harness
// hook, so a test can prove a refused community is not sent again.
func (d *Device) SNMPRequests() int64 { return d.snmpRequests.Load() }

// Shutdown closes the socket, which ends the read loop.
func (l *snmpLoop) Shutdown() { _ = l.conn.Close() }

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

// snmpLogger drops the SNMP library's per-request debug and info output
// (hits report through RequestPublisher) but keeps its warnings and errors,
// such as a value it cannot encode for the requested OID.
type snmpLogger struct{ GoSNMPServer.ILogger }

func (snmpLogger) Warn(args ...interface{}) { slog.Warn("snmp: " + fmt.Sprint(args...)) }
func (snmpLogger) Warnf(format string, args ...interface{}) {
	slog.Warn("snmp: " + fmt.Sprintf(format, args...))
}
func (snmpLogger) Warning(args ...interface{}) { slog.Warn("snmp: " + fmt.Sprint(args...)) }
func (snmpLogger) Error(args ...interface{})   { slog.Error("snmp: " + fmt.Sprint(args...)) }
func (snmpLogger) Errorf(format string, args ...interface{}) {
	slog.Error("snmp: " + fmt.Sprintf(format, args...))
}
func (snmpLogger) Errorln(args ...interface{}) { slog.Error("snmp: " + fmt.Sprint(args...)) }
