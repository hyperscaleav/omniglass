package sim

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gosnmp/gosnmp"
	"github.com/slayercat/GoSNMPServer"

	"github.com/hyperscaleav/omniglass/sim/surface"
)

// The seams through which the protocol servers serve a Go-declared surface
// (sim/surface, #2831) beside a family's own commands, routes and OIDs.
// Attach before the device serves. A surface is a function of its family's
// typed state; the device's state map only carries what a surface reports it
// changed, so subscribers, the diff stream and the version see a command
// a surface answers as they see any other write.

// AttachCommands serves a Go-declared line-protocol surface on every
// persistent face the device has, after the family's own commands.
func (d *Device) AttachCommands(c surface.Commander) { d.goCommands = append(d.goCommands, c) }

// AttachRoute serves a Go-declared HTTP route, ahead of a family's own.
func (d *Device) AttachRoute(r surface.Route) { d.goRoutes = append(d.goRoutes, r) }

// AttachOIDs serves Go-declared SNMP scalars beside a family's own.
func (d *Device) AttachOIDs(oids ...surface.OID) { d.goOIDs = append(d.goOIDs, oids...) }

// runGoCommand answers a line from the attached Go surfaces and applies the
// changes the reply reports, as a command's mutations are applied.
func (d *Device) runGoCommand(cmd string) (CommandResult, bool, error) {
	for _, c := range d.goCommands {
		reply, ok := c.Handle(cmd)
		if !ok {
			continue
		}
		if len(reply.Changes) > 0 {
			diffs := make(map[string]any, len(reply.Changes))
			d.mu.Lock()
			for _, ch := range reply.Changes {
				if err := SetPath(d.state, ch.Path, ch.Value); err != nil {
					d.mu.Unlock()
					return CommandResult{}, true, fmt.Errorf("setting %q: %w", ch.Path, err)
				}
				diffs[ch.Path] = ch.Value
			}
			d.Version++
			if d.DiffPublisher != nil {
				d.DiffPublisher(StateChange{DeviceID: d.DeviceID, Version: d.Version, Diffs: diffs, Cause: "command:" + cmd})
			}
			d.fanoutToSessions(diffs)
			d.mu.Unlock()
		}
		return CommandResult{
			Response:   reply.Text,
			Action:     CommandAction(reply.Action),
			Subject:    reply.Subject,
			Token:      reply.Token,
			IntervalMs: reply.IntervalMs,
		}, true, nil
	}
	return CommandResult{}, false, nil
}

// serveGoRoute answers the request from an attached Go route, reporting
// whether one matched.
func (d *Device) serveGoRoute(w http.ResponseWriter, r *http.Request) bool {
	for _, route := range d.goRoutes {
		if r.Method != route.Method || r.URL.Path != route.Path {
			continue
		}
		resp := route.Serve()
		if resp.ContentType != "" {
			w.Header().Set("Content-Type", resp.ContentType)
		}
		if resp.Status == 0 {
			resp.Status = http.StatusOK
		}
		w.WriteHeader(resp.Status)
		_, _ = w.Write(resp.Body)
		return true
	}
	return false
}

// goOID is the attached Go OID with this address, the leading dot optional.
func (d *Device) goOID(oid string) (surface.OID, bool) {
	oid = strings.TrimPrefix(oid, ".")
	for _, o := range d.goOIDs {
		if strings.TrimPrefix(o.OID, ".") == oid {
			return o, true
		}
	}
	return surface.OID{}, false
}

// goOIDEntries turns the attached Go OIDs into agent entries.
func (d *Device) goOIDEntries(port int, community, listenerAddr string) []*GoSNMPServer.PDUValueControlItem {
	items := make([]*GoSNMPServer.PDUValueControlItem, 0, len(d.goOIDs))
	for _, o := range d.goOIDs {
		o := o
		asn1Type := asn1Kind(o.Kind)
		items = append(items, &GoSNMPServer.PDUValueControlItem{
			OID:  strings.TrimPrefix(o.OID, "."),
			Type: asn1Type,
			OnGet: func() (interface{}, error) {
				return d.reportOID(o.OID, "", asn1Type, port, community, listenerAddr,
					func() (interface{}, error) { return o.Get(), nil })
			},
		})
	}
	return items
}

func asn1Kind(k surface.Kind) gosnmp.Asn1BER {
	switch k {
	case surface.KindInteger:
		return gosnmp.Integer
	case surface.KindGauge32:
		return gosnmp.Gauge32
	case surface.KindCounter32:
		return gosnmp.Counter32
	case surface.KindCounter64:
		return gosnmp.Counter64
	case surface.KindTimeTicks:
		return gosnmp.TimeTicks
	default:
		return gosnmp.OctetString
	}
}
