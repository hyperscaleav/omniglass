package sim

import (
	"fmt"
	"log/slog"
	"net"
	"strings"
)

// StartUDP binds a bare echo responder on the device's loopback IP:port.
// Echoes every inbound datagram back to the sender. Useful as a minimal
// proof of UDP reachability and for vendor-specific request/response
// protocols where the caller already controls both sides of the wire.
func (d *Device) StartUDP(port int) error {
	addr := fmt.Sprintf("%s:%d", d.LoopbackIP.String(), port)
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}
	d.udpConn = conn

	go func() {
		slog.Debug("device UDP started", "device", d.Hostname, "addr", addr)
		buf := make([]byte, 4096)
		for {
			n, from, err := conn.ReadFrom(buf)
			if err != nil {
				if !strings.Contains(err.Error(), "closed") {
					slog.Error("device UDP read error", "device", d.Hostname, "error", err)
				}
				return
			}
			// Echo everything back.
			_, _ = conn.WriteTo(buf[:n], from)
			if d.Logger != nil {
				d.Logger(RequestLog{
					DeviceID:  d.DeviceID,
					Hostname:  d.Hostname,
					Protocol:  "udp",
					Port:      port,
					Method:    "ECHO",
					Path:      fmt.Sprintf("%d bytes", n),
					Remote:    from.String(),
					LocalAddr: fmt.Sprintf("%s:%d", d.LoopbackIP.String(), port),
					Status:    200,
				})
			}
		}
	}()
	return nil
}

// StopUDP closes the UDP echo responder.
func (d *Device) StopUDP() {
	if d.udpConn != nil {
		d.udpConn.Close()
	}
}
