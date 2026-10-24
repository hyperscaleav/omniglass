package sim

import (
	"net"
	"net/netip"
)

// EphemeralPorts is the port map that binds every face on port 0, so the
// kernel hands each one a free port, and the face reports the port it got
// (Device.Faces). Tests serve this way, so two runs on one host never
// collide over a port. Its counterpart is the fixed map (nil, or
// PrivilegedPortShift): every face at the port its profile declares, which is
// how the operator-facing simulator serves (omniglass sim run, the dev fleet),
// because an operator, a seeded endpoint and the docs all name those ports.
// Both go through the one bind path; only the port asked for differs.
func EphemeralPorts(int) int { return 0 }

// Face is one listener a device serves: the profile interface it realizes and
// the address it bound.
type Face struct {
	Name     string         // the profile interface's name
	Protocol string         // http, ssh, telnet, tcp, snmp or udp
	TLS      bool           // served behind TLS: an https face, or a tcp line face over tls (#1035)
	Declared int            // the port the profile declares
	Addr     netip.AddrPort // the address bound: under EphemeralPorts, its port is the kernel's pick
}

// Faces returns the faces the engine started on the device, in profile
// order, each with the address it bound. A face stopped later keeps its
// entry: a test can check its address went dark, or start it again on the
// same port to model an outage the client recovers from.
func (d *Device) Faces() []Face {
	d.faceMu.Lock()
	defer d.faceMu.Unlock()
	return append([]Face(nil), d.faces...)
}

// Face returns the face realizing the named profile interface.
func (d *Device) Face(name string) (Face, bool) {
	d.faceMu.Lock()
	defer d.faceMu.Unlock()
	for _, f := range d.faces {
		if f.Name == name {
			return f, true
		}
	}
	return Face{}, false
}

func (d *Device) addFace(f Face) {
	d.faceMu.Lock()
	d.faces = append(d.faces, f)
	d.faceMu.Unlock()
}

// addrPort reads a listener's bound address as an address and port, an IPv4
// address in its four-byte form.
func addrPort(a net.Addr) netip.AddrPort {
	var ap netip.AddrPort
	switch a := a.(type) {
	case *net.TCPAddr:
		ap = a.AddrPort()
	case *net.UDPAddr:
		ap = a.AddrPort()
	default:
		ap, _ = netip.ParseAddrPort(a.String())
	}
	return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
}

// boundPort is the port a listener bound.
func boundPort(a net.Addr) int { return int(addrPort(a).Port()) }
