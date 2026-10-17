package sim

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"
)

// StartTelnet binds a plain-TCP telnet server on the device's loopback
// IP:port.
//
// Default posture is strict: a client must enter the configured
// defaultUser and defaultPassword at the login prompts. Set
// permissive to true to accept any user/password, which models the
// legacy demo posture and is useful for fixture profiles or tests.
//
// Backwards-compatibility fallback: if defaultPassword is empty and
// permissive is false, the listener falls back to the legacy
// accept-anything behavior and logs a warning. This keeps older
// fixture profiles that relied on the empty-password convention
// working while flagging them so they can be updated.
//
// After login, reads lines and dispatches through Device.RunCommand.
// `exit` / `quit` / EOF close the session.
func (d *Device) StartTelnet(port int, defaultUser, defaultPassword string, permissive bool) error {
	addr := fmt.Sprintf("%s:%d", d.LoopbackIP.String(), port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}
	tracked := trackConns(lis)
	d.telnetListener = tracked

	effectivePermissive := permissive
	if !effectivePermissive && defaultPassword == "" {
		slog.Warn("telnet listener falling back to permissive auth, default_password is empty",
			"device", d.Hostname, "default_user", defaultUser,
		)
		effectivePermissive = true
	}

	go func() {
		slog.Info("device telnet started", "device", d.Hostname, "addr", addr)
		for {
			conn, err := tracked.Accept()
			if err != nil {
				if !strings.Contains(err.Error(), "closed") {
					slog.Error("device telnet accept error", "device", d.Hostname, "error", err)
				}
				return
			}
			go d.serveTelnetConn(conn, defaultUser, defaultPassword, effectivePermissive, port)
		}
	}()
	return nil
}

// StopTelnet closes the telnet listener and every connection it accepted.
func (d *Device) StopTelnet() {
	if d.telnetListener != nil {
		d.telnetListener.Close()
	}
}

// serveTelnetConn runs one telnet session: option negotiation → banner →
// login → password → command loop. Uses the device's RunCommand for
// dispatch (same machinery as SSH). The prompt is always `<hostname>> `.
func (d *Device) serveTelnetConn(conn net.Conn, defaultUser, defaultPassword string, permissive bool, port int) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(60 * time.Second))

	// Minimal option negotiation so modern clients don't block on DO/WILL
	// exchanges. We advertise that we WILL echo and WILL suppress-go-ahead
	// and refuse everything else the client asks of us.
	const (
		IAC  = 0xFF
		WILL = 0xFB
		DO   = 0xFD
		DONT = 0xFE
		WONT = 0xFC
	)
	const (
		OPT_ECHO = 0x01
		OPT_SGA  = 0x03
	)
	// Send initial negotiation.
	_, _ = conn.Write([]byte{IAC, WILL, OPT_ECHO, IAC, WILL, OPT_SGA})

	rd := bufio.NewReader(telnetReader{r: conn})
	w := conn

	banner := fmt.Sprintf("%s\r\n", d.Hostname)
	_, _ = io.WriteString(w, banner)

	// Login prompt.
	_, _ = io.WriteString(w, "login: ")
	user, err := readLine(rd, 10*time.Second, conn)
	if err != nil {
		return
	}

	_, _ = io.WriteString(w, "Password: ")
	pass, err := readLine(rd, 10*time.Second, conn)
	if err != nil {
		return
	}
	_, _ = io.WriteString(w, "\r\n")

	if !telnetAuthOK(user, pass, defaultUser, defaultPassword, permissive) {
		_, _ = io.WriteString(w, "auth rejected\r\n")
		return
	}

	remote := conn.RemoteAddr().String()

	// Persistent mode: hand the connection off to the shared shell
	// loop. Subscriptions and pushes interleave with command
	// responses on the same socket. Bypasses the prompt-per-line
	// loop below, TTP-shaped protocols are prompt-less by design.
	if reg := d.telnetSession; reg != nil {
		_ = conn.SetDeadline(time.Time{})
		sess, err := reg.Open(remote)
		if err != nil {
			_, _ = io.WriteString(w, "session rejected: "+err.Error()+"\r\n")
			return
		}
		defer reg.Close(sess)
		runShellSession(d, sess, rd, w, port, "telnet")
		return
	}

	prompt := fmt.Sprintf("%s> ", d.Hostname)
	for {
		_, _ = io.WriteString(w, prompt)
		line, err := readLine(rd, 60*time.Second, conn)
		if err != nil {
			return
		}
		cmd := strings.TrimSpace(line)
		if cmd == "" {
			continue
		}
		if cmd == "exit" || cmd == "quit" {
			_, _ = io.WriteString(w, "bye\r\n")
			return
		}
		start := time.Now()
		result, ok, rerr := d.RunCommand(cmd)
		elapsed := time.Since(start)
		status := 200
		if !ok {
			_, _ = io.WriteString(w, "unknown command: "+cmd+"\r\n")
			status = 404
		} else if rerr != nil {
			_, _ = io.WriteString(w, "template error: "+rerr.Error()+"\r\n")
			status = 500
		} else {
			// Normalize LF to CRLF for classic telnet clients.
			_, _ = io.WriteString(w, strings.ReplaceAll(result.Response, "\n", "\r\n"))
			if !strings.HasSuffix(result.Response, "\n") {
				_, _ = io.WriteString(w, "\r\n")
			}
		}
		if d.Logger != nil {
			d.Logger(RequestLog{
				DeviceID:   d.DeviceID,
				Hostname:   d.Hostname,
				Protocol:   "telnet",
				Port:       port,
				Method:     "EXEC",
				Path:       cmd,
				Remote:     remote,
				LocalAddr:  fmt.Sprintf("%s:%d", d.LoopbackIP.String(), port),
				Status:     status,
				DurationMs: elapsed.Milliseconds(),
			})
		}
	}
}

func telnetAuthOK(user, pass, defaultUser, defaultPassword string, permissive bool) bool {
	if permissive {
		return true
	}
	return user == defaultUser && pass == defaultPassword
}

// readLine reads a single CR/LF-terminated line from rd with a deadline.
// Returns the line without trailing newline characters.
func readLine(rd *bufio.Reader, timeout time.Duration, conn net.Conn) (string, error) {
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	line, err := rd.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// telnetReader strips IAC option negotiation bytes inbound so the
// bufio.Reader above only sees plain data.
type telnetReader struct{ r io.Reader }

func (t telnetReader) Read(p []byte) (int, error) {
	buf := make([]byte, len(p))
	n, err := t.r.Read(buf)
	if n <= 0 {
		return n, err
	}
	out := 0
	for i := 0; i < n; i++ {
		if buf[i] == 0xFF && i+2 < n {
			// Skip IAC option triple inbound.
			i += 2
			continue
		}
		p[out] = buf[i]
		out++
	}
	return out, err
}
