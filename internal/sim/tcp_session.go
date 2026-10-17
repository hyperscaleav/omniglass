package sim

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
	"strings"
)

// StartTCPSession binds a raw-TCP persistent shell on the device's
// loopback IP:port. Unlike the SSH and Telnet listeners, raw TCP has
// no auth ceremony, no IAC negotiation, and no shell channel
// handshake, every accept hands directly off to runShellSession,
// which honors the interface's configured wire-format quirks
// (LineEnding, FrameStart/End, StripChecksumBytes) the same way it
// does for SSH and Telnet.
//
// Mirrors the SSH and Telnet listener Start* shape so the engine's
// dispatch table needs only one extra case. The session registry must
// be attached via AttachTCPSession before calling StartTCPSession,
// otherwise the listener accepts connections and immediately closes
// them with a slog.Warn, refusing connections without a registry
// is friendlier than serving them in a degraded "no sessions" mode.
func (d *Device) StartTCPSession(port int) error {
	addr := fmt.Sprintf("%s:%d", d.LoopbackIP.String(), port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}
	tracked := trackConns(lis)
	d.tcpSessionListener = tracked

	go func() {
		slog.Info("device TCP-session started", "device", d.Hostname, "addr", addr)
		for {
			conn, err := tracked.Accept()
			if err != nil {
				if !strings.Contains(err.Error(), "closed") {
					slog.Error("device TCP-session accept error", "device", d.Hostname, "error", err)
				}
				return
			}
			go d.serveTCPSession(conn, port)
		}
	}()
	return nil
}

// StopTCPSession closes the raw-TCP listener and every connection it accepted.
func (d *Device) StopTCPSession() {
	if d.tcpSessionListener != nil {
		d.tcpSessionListener.Close()
	}
}

// serveTCPSession is the per-connection handler. Opens a session
// against the device's TCP SessionRegistry and runs the shared shell
// loop. No deadline is set on the conn, persistent sessions hold
// open across many commands and welcome banners arrive immediately.
func (d *Device) serveTCPSession(conn net.Conn, port int) {
	defer conn.Close()

	reg := d.tcpSession
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

// tcpPort derives the port the TCP-session listener bound to. Matches
// the sshPort / telnet port pattern used elsewhere for consistent
// event-log Port fields. Best-effort read off the listener's address.
func (d *Device) tcpPort() int {
	if d.tcpSessionListener == nil {
		return 0
	}
	if tcp, ok := d.tcpSessionListener.Addr().(*net.TCPAddr); ok {
		return tcp.Port
	}
	return 0
}
