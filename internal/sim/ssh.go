package sim

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// StartSSH binds an SSH server on the device's loopback IP:port.
//
// Default posture is strict: a client must present the configured
// defaultUser and defaultPassword to authenticate. Set permissive to
// true to accept any user and password, which is useful for tests or
// fixture profiles that intentionally model an open device.
//
// Backwards-compatibility fallback: if defaultPassword is empty and
// permissive is false, the listener falls back to the legacy
// accept-anything behavior and logs a warning. This keeps older
// fixture profiles that relied on the empty-password convention
// working while flagging them so they can be updated.
//
// Only handles `exec` requests, `shell` and `pty-req` are rejected so
// clients must run one command at a time.
//
// Inbound commands are routed through Device.RunCommand against the
// profile-declared Commands. Unknown commands return a one-line error
// and a non-zero exit status.
func (d *Device) StartSSH(port int, defaultUser, defaultPassword string, permissive bool) error {
	_, err := d.startSSH(port, defaultUser, defaultPassword, permissive)
	return err
}

// startSSH binds and serves the SSH listener and returns the address it
// bound.
func (d *Device) startSSH(port int, defaultUser, defaultPassword string, permissive bool) (net.Addr, error) {
	if d.sshHostKey == nil {
		k, err := deviceSSHHostKey(d.DeviceID)
		if err != nil {
			return nil, fmt.Errorf("host key: %w", err)
		}
		d.sshHostKey = k
	}
	hostKey := d.sshHostKey

	effectivePermissive := permissive
	if !effectivePermissive && defaultPassword == "" {
		slog.Warn("ssh listener falling back to permissive auth, default_password is empty",
			"device", d.Hostname, "default_user", defaultUser,
		)
		effectivePermissive = true
	}

	cfg := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			d.sshPasswordTries.Add(1)
			if effectivePermissive {
				return nil, nil
			}
			if conn.User() == defaultUser && string(pass) == defaultPassword {
				return nil, nil
			}
			return nil, fmt.Errorf("auth rejected")
		},
	}
	cfg.AddHostKey(hostKey)

	addr := fmt.Sprintf("%s:%d", d.LoopbackIP.String(), port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", addr, err)
	}
	addr = lis.Addr().String()
	if reg := d.sshSession; reg != nil {
		reg.setPort(boundPort(lis.Addr()))
	}
	tracked := trackConns(lis, d.logConnect)
	d.sshListener = tracked

	go func() {
		slog.Debug("device SSH started", "device", d.Hostname, "addr", addr)
		for {
			conn, err := tracked.Accept()
			if err != nil {
				if !strings.Contains(err.Error(), "closed") {
					slog.Error("device SSH accept error", "device", d.Hostname, "error", err)
				}
				return
			}
			go d.serveSSHConn(conn, cfg)
		}
	}()
	return lis.Addr(), nil
}

// StopSSH closes the SSH listener and every connection it accepted.
func (d *Device) StopSSH() {
	if d.sshListener != nil {
		d.sshListener.Close()
	}
}

// serveSSHConn completes the SSH handshake then handles session
// channels. Anything other than `exec` is rejected, except when the
// device has an SSH SessionRegistry bound, in that case `shell`
// channels open a persistent session.
func (d *Device) serveSSHConn(conn net.Conn, cfg *ssh.ServerConfig) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	// Capture the client address before the handshake hands us an
	// ssh.ServerConn; serveSSHSession only sees the channel, not the
	// underlying conn, so we thread this through explicitly for the
	// RequestLog.
	remote := conn.RemoteAddr().String()

	_, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		slog.Debug("ssh handshake failed", "device", d.Hostname, "error", err)
		return
	}
	// Persistent sessions hold open across many commands; the
	// 30-second handshake deadline would terminate them. Clear it
	// once the handshake is done so shell mode lives as long as the
	// client wants. Exec mode does its own implicit short lifecycle
	// per request, so the cleared deadline costs nothing there.
	_ = conn.SetDeadline(time.Time{})
	go ssh.DiscardRequests(reqs)

	// Source labelling is left to the hit consumer, fed off the raw
	// peer address. We deliberately don't peek at the SSH banner for
	// self-identification, that would be a second source of truth
	// that can quietly mask a missing label.

	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			newChan.Reject(ssh.UnknownChannelType, "only session channels supported")
			continue
		}
		ch, reqs, err := newChan.Accept()
		if err != nil {
			continue
		}
		go d.serveSSHSession(ch, reqs, remote)
	}
}

// serveSSHSession handles a single session channel. Responds to `exec`
// requests by running the command through the device's command table.
// When the device has a SessionRegistry bound (persistent: true),
// `shell` requests open a long-lived shell session; otherwise `shell`
// is rejected. `pty-req` is accepted as a no-op when shell is enabled
// so clients that always pair them still negotiate cleanly.
func (d *Device) serveSSHSession(ch ssh.Channel, reqs <-chan *ssh.Request, remote string) {
	defer ch.Close()
	persistent := d.sshSession != nil
	for req := range reqs {
		switch req.Type {
		case "shell":
			if !persistent {
				_ = req.Reply(false, nil)
				continue
			}
			_ = req.Reply(true, nil)
			d.runSSHShell(ch, remote)
			return
		case "pty-req":
			// We don't allocate a real pty; reply success when shell
			// mode is available so a client that insists on pty-req
			// before shell still proceeds. Reply false in exec mode
			// to preserve the historical strict posture.
			_ = req.Reply(persistent, nil)
		case "exec":
			// ssh wire format: four-byte big-endian length prefix, then
			// the command string.
			if len(req.Payload) < 4 {
				_ = req.Reply(false, nil)
				return
			}
			cmdLen := binary.BigEndian.Uint32(req.Payload[:4])
			if uint32(len(req.Payload)-4) < cmdLen {
				_ = req.Reply(false, nil)
				return
			}
			cmd := string(req.Payload[4 : 4+cmdLen])
			_ = req.Reply(true, nil)

			start := time.Now()
			result, ok, rerr := d.RunCommand(cmd)
			elapsed := time.Since(start)
			exitStatus := uint32(0)
			if !ok {
				_, _ = io.WriteString(ch.Stderr(), "unknown command: "+cmd+"\n")
				exitStatus = 127
			} else if rerr != nil {
				_, _ = io.WriteString(ch.Stderr(), "template error: "+rerr.Error()+"\n")
				exitStatus = 1
			} else {
				_, _ = io.WriteString(ch, result.Response)
			}

			// CloseWrite flushes stdout/stderr and signals EOF to the client.
			// Without this the client's Wait() can return on exit-status
			// before the read side has drained the stdout buffer, which
			// shows up as empty output under tight loopback timing.
			_ = ch.CloseWrite()

			// exit-status is a uint32 in SSH wire format.
			statusBytes := make([]byte, 4)
			binary.BigEndian.PutUint32(statusBytes, exitStatus)
			_, _ = ch.SendRequest("exit-status", false, statusBytes)

			if d.Logger != nil {
				// Map SSH exit status into the HTTP-ish Status field the
				// event bus renders: 200 on success, 500 otherwise. The
				// field is shared across protocols for consistency.
				status := 200
				if exitStatus != 0 {
					status = 500
				}
				d.Logger(RequestLog{
					DeviceID:   d.DeviceID,
					Hostname:   d.Hostname,
					Protocol:   "ssh",
					Port:       d.sshPort(),
					Method:     "EXEC",
					Path:       cmd,
					Remote:     remote,
					LocalAddr:  fmt.Sprintf("%s:%d", d.LoopbackIP.String(), d.sshPort()),
					Status:     status,
					DurationMs: elapsed.Milliseconds(),
				})
			}
			return
		default:
			_ = req.Reply(false, nil)
		}
	}
}

// runSSHShell wires an accepted SSH `shell` channel to the shared
// session loop. Opens a Session against the SSH SessionRegistry,
// shuttles bytes both ways, and reports session_open / session_close
// via the registry's bus. Buffered reader normalizes line termination
// so shellsession sees one command per line whether the client sends
// CR, LF, or CRLF.
func (d *Device) runSSHShell(ch ssh.Channel, remote string) {
	reg := d.sshSession
	sess, err := reg.Open(remote)
	if err != nil {
		_, _ = io.WriteString(ch.Stderr(), "session rejected: "+err.Error()+"\r\n")
		return
	}
	defer reg.Close(sess)

	// Clear the per-channel deadline; persistent sessions hold open
	// across many commands and we don't want a 30s handshake timeout
	// to murder them mid-session.
	rd := bufio.NewReader(ch)
	runShellSession(d, sess, rd, ch, d.sshPort(), "ssh")
}

// SSHPasswordTries counts the passwords the ssh face was sent, right or
// wrong: a test reads how often a credential was tried (#1060).
func (d *Device) SSHPasswordTries() int64 { return d.sshPasswordTries.Load() }

// RotateSSHHostKey replaces the device's SSH host key; the next StartSSH
// serves the new one. A real device's key changes when it is replaced or
// reimaged, or when something sits in the middle: the collector's pin must
// notice (#1032).
func (d *Device) RotateSSHHostKey() error {
	k, err := generateHostKey()
	if err != nil {
		return err
	}
	d.sshHostKey = k
	return nil
}

// SSHHostKey is the device's current SSH host key, nil before StartSSH.
func (d *Device) SSHHostKey() ssh.PublicKey {
	if d.sshHostKey == nil {
		return nil
	}
	return d.sshHostKey.PublicKey()
}

// sshPort derives the port number the SSH listener bound to. This is a
// best-effort read off the listener address.
func (d *Device) sshPort() int {
	if d.sshListener == nil {
		return 22
	}
	if tcp, ok := d.sshListener.Addr().(*net.TCPAddr); ok {
		return tcp.Port
	}
	return 22
}

// generateHostKey produces an ephemeral ed25519 host key. Demo posture,
// the client ignores host key verification with InsecureIgnoreHostKey.
// deviceSSHHostKey is the device's ssh host key, derived from its id (#1370)
// as its https key is, so a key a node pinned on first use survives a
// simulator restart, as a real device keeps its key across a reboot. A
// rotation (a reimaged device) draws a random one.
func deviceSSHHostKey(deviceID string) (ssh.Signer, error) {
	seed := sha256.Sum256([]byte("omniglass-sim-ssh\x00" + deviceID))
	return ssh.NewSignerFromKey(ed25519.NewKeyFromSeed(seed[:]))
}

func generateHostKey() (ssh.Signer, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(priv)
}
