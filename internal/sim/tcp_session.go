package sim

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"strings"
)

// StartTCPSession binds a raw-TCP persistent shell on the device's
// loopback IP for its first attached registry. The engine starts each
// persistent tcp interface with its own registry (startTCPSession).
func (d *Device) StartTCPSession(port int) error {
	_, err := d.startTCPSession(port, d.TCPSession(), nil)
	return err
}

// startTCPSession binds one raw-TCP listener serving one registry's
// sessions and returns the address it bound; with a tls config the line
// protocol runs behind a TLS handshake (#1035). A nil registry is a
// misconfiguration: every connect is closed.
func (d *Device) startTCPSession(port int, reg *SessionRegistry, tlsConfig *tls.Config) (net.Addr, error) {
	addr := fmt.Sprintf("%s:%d", d.LoopbackIP.String(), port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", addr, err)
	}
	addr = lis.Addr().String()
	port = boundPort(lis.Addr()) // what its hits and sessions report
	if reg != nil {
		reg.setPort(port)
	}
	tracked := trackConns(lis, d.logConnect)
	d.tcpMu.Lock()
	d.tcpListeners = append(d.tcpListeners, tracked)
	d.tcpMu.Unlock()
	var serving net.Listener = tracked
	if tlsConfig != nil {
		serving = tls.NewListener(tracked, tlsConfig)
	}

	go func() {
		slog.Debug("device TCP-session started", "device", d.Hostname, "addr", addr, "tls", tlsConfig != nil)
		for {
			conn, err := serving.Accept()
			if err != nil {
				if !strings.Contains(err.Error(), "closed") {
					slog.Error("device TCP-session accept error", "device", d.Hostname, "error", err)
				}
				return
			}
			go d.serveTCPSession(conn, port, reg)
		}
	}()
	return lis.Addr(), nil
}

// StopTCPSession closes every raw-TCP listener and every connection they
// accepted.
func (d *Device) StopTCPSession() {
	d.tcpMu.Lock()
	ls := d.tcpListeners
	d.tcpListeners = nil
	d.tcpMu.Unlock()
	for _, l := range ls {
		_ = l.Close()
	}
}

// TCPSessionAddrs returns the bound address of each raw-TCP listener, in
// start order.
func (d *Device) TCPSessionAddrs() []net.Addr {
	d.tcpMu.Lock()
	defer d.tcpMu.Unlock()
	out := make([]net.Addr, 0, len(d.tcpListeners))
	for _, l := range d.tcpListeners {
		out = append(out, l.Addr())
	}
	return out
}

// serveTCPSession is the per-connection handler. Opens a session
// against the device's TCP SessionRegistry and runs the shared shell
// loop. No deadline is set on the conn, persistent sessions hold
// open across many commands and welcome banners arrive immediately.
func (d *Device) serveTCPSession(conn net.Conn, port int, reg *SessionRegistry) {
	defer conn.Close()

	if reg == nil {
		// Listener bound without a registry, operator misconfig.
		// Log loudly and close; profiles that declare persistent: true
		// on a tcp interface MUST come with subjects + commands so
		// the engine builds a registry. Without one, every connect
		// would dispatch into nothing.
		slog.Warn("TCP-session connection rejected: no registry attached",
			"device", d.Hostname, "port", port)
		return
	}

	remote := conn.RemoteAddr().String()
	sess, err := reg.Open(remote)
	if err != nil {
		slog.Debug("TCP-session open rejected",
			"device", d.Hostname, "remote", remote, "error", err)
		return
	}
	defer reg.Close(sess)

	rd := bufio.NewReader(conn)
	runShellSession(d, sess, rd, conn, port, "tcp")
}
