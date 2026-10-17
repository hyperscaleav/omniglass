package sim

import "time"

// Hit is one request a simulated device answered, on any protocol: the
// shape a consumer needs to show that a device was reached (by a collector,
// an operator, a test), without the per-protocol detail of RequestLog or
// RequestEvent.
type Hit struct {
	DeviceID string
	Hostname string
	Protocol string // http, snmp, ssh, telnet, tcp, udp
	Method   string
	Path     string
	Port     int
	Status   int
	Remote   string
	At       time.Time
}

func hitFromLog(l RequestLog) Hit {
	proto := l.Protocol
	if proto == "" {
		proto = "http" // the HTTP listener predates the Protocol tag
	}
	return Hit{DeviceID: l.DeviceID, Hostname: l.Hostname, Protocol: proto, Method: l.Method,
		Path: l.Path, Port: l.Port, Status: l.Status, Remote: l.Remote, At: time.Now()}
}

func hitFromEvent(ev RequestEvent) Hit {
	return Hit{DeviceID: ev.DeviceID, Hostname: ev.Hostname, Protocol: ev.Protocol, Method: ev.Method,
		Path: ev.Path, Port: ev.Port, Status: ev.Status, Remote: ev.Remote, At: time.Now()}
}
