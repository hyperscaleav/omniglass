package sim

import (
	"bytes"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestSSHExec_DeclaredCommand(t *testing.T) {
	dev := NewDevice("dev-1", "test-host", net.ParseIP("127.0.0.1"), "test-profile", 1,
		map[string]any{
			"status": map[string]any{
				"device": map[string]any{"power": "on"},
			},
		})

	// Profile-style command: renders a one-line output.
	const cmd = "xstatus systemunit"
	const tmpl = `power={{ .State | get "status" | get "device" | get "power" }}` + "\n"
	if err := dev.AddCommand(cmd, "x.tmpl", tmpl, nil); err != nil {
		t.Fatalf("AddCommand: %v", err)
	}

	if err := dev.StartSSH(0, "admin", "admin", false); err != nil {
		t.Fatalf("StartSSH: %v", err)
	}
	t.Cleanup(dev.StopSSH)

	addr := dev.sshListener.Addr().String()
	t.Logf("ssh addr: %s", addr)

	out, ec, err := sshExec(addr, "admin", "admin", cmd)
	if err != nil {
		t.Fatalf("sshExec: %v", err)
	}
	if ec != 0 {
		t.Errorf("exit status = %d, want 0; output=%q", ec, out)
	}
	if !strings.Contains(out, "power=on") {
		t.Errorf("stdout = %q, want to contain power=on", out)
	}
}

func TestSSHExec_UnknownCommandExits127(t *testing.T) {
	dev := NewDevice("dev-2", "test-host", net.ParseIP("127.0.0.1"), "test-profile", 1, map[string]any{})
	if err := dev.StartSSH(0, "admin", "admin", false); err != nil {
		t.Fatalf("StartSSH: %v", err)
	}
	t.Cleanup(dev.StopSSH)

	addr := dev.sshListener.Addr().String()

	_, ec, err := sshExec(addr, "admin", "admin", "no such command")
	if err != nil {
		t.Fatalf("sshExec: %v", err)
	}
	if ec != 127 {
		t.Errorf("unknown command exit status = %d, want 127", ec)
	}
}

// TestSSHAuth_StrictRejectsWrongPassword pins the new default posture:
// when StartSSH is invoked with a non-empty password and permissive
// is false, a client that presents the wrong password is rejected at
// the handshake.
func TestSSHAuth_StrictRejectsWrongPassword(t *testing.T) {
	dev := NewDevice("dev-auth-1", "test-host", net.ParseIP("127.0.0.1"), "test-profile", 1, map[string]any{})
	if err := dev.StartSSH(0, "admin", "kestrel", false); err != nil {
		t.Fatalf("StartSSH: %v", err)
	}
	t.Cleanup(dev.StopSSH)

	addr := dev.sshListener.Addr().String()
	cfg := &ssh.ClientConfig{
		User:            "admin",
		Auth:            []ssh.AuthMethod{ssh.Password("not-the-password")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, cfg)
	if err == nil {
		client.Close()
		t.Fatalf("expected handshake to fail, got nil")
	}
}

// TestSSHAuth_StrictAcceptsCorrectPassword complements the rejection
// test: the same listener accepts a client that presents the matching
// user and password.
func TestSSHAuth_StrictAcceptsCorrectPassword(t *testing.T) {
	dev := NewDevice("dev-auth-2", "test-host", net.ParseIP("127.0.0.1"), "test-profile", 1, map[string]any{})
	if err := dev.StartSSH(0, "admin", "kestrel", false); err != nil {
		t.Fatalf("StartSSH: %v", err)
	}
	t.Cleanup(dev.StopSSH)

	addr := dev.sshListener.Addr().String()
	cfg := &ssh.ClientConfig{
		User:            "admin",
		Auth:            []ssh.AuthMethod{ssh.Password("kestrel")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		t.Fatalf("expected handshake to succeed, got %v", err)
	}
	client.Close()
}

// TestSSHAuth_PermissiveAcceptsAnything pins the opt-in escape hatch:
// when the listener is started with permissive=true, any password is
// accepted. This preserves the legacy demo posture for fixture
// profiles or tests that want to model an open device.
func TestSSHAuth_PermissiveAcceptsAnything(t *testing.T) {
	dev := NewDevice("dev-auth-3", "test-host", net.ParseIP("127.0.0.1"), "test-profile", 1, map[string]any{})
	if err := dev.StartSSH(0, "admin", "kestrel", true); err != nil {
		t.Fatalf("StartSSH: %v", err)
	}
	t.Cleanup(dev.StopSSH)

	addr := dev.sshListener.Addr().String()
	cfg := &ssh.ClientConfig{
		User:            "anyone",
		Auth:            []ssh.AuthMethod{ssh.Password("anything")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		t.Fatalf("expected permissive handshake to succeed, got %v", err)
	}
	client.Close()
}

// TestSSHAuth_EmptyPasswordFallsBackPermissive pins the
// backwards-compatibility branch: when default_password is empty and
// permissive is not explicitly set, the listener accepts anything and
// logs a warning. This keeps older fixture profiles working until they
// are updated to either declare a password or opt into permissive.
func TestSSHAuth_EmptyPasswordFallsBackPermissive(t *testing.T) {
	dev := NewDevice("dev-auth-4", "test-host", net.ParseIP("127.0.0.1"), "test-profile", 1, map[string]any{})
	if err := dev.StartSSH(0, "admin", "", false); err != nil {
		t.Fatalf("StartSSH: %v", err)
	}
	t.Cleanup(dev.StopSSH)

	addr := dev.sshListener.Addr().String()
	cfg := &ssh.ClientConfig{
		User:            "anyone",
		Auth:            []ssh.AuthMethod{ssh.Password("anything")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		t.Fatalf("expected fallback handshake to succeed, got %v", err)
	}
	client.Close()
}

// sshExec runs one command over SSH and returns stdout + exit code.
// Uses Output() via an explicit Start/Wait split so the stdout copy
// goroutine runs synchronously via io.ReadAll, avoiding a Run/Wait race
// where Wait returns on exit-status before the stdout buffer is drained.
func sshExec(addr, user, pass, cmd string) (string, int, error) {
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(pass)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return "", 0, err
	}
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		return "", 0, err
	}
	defer sess.Close()

	stdoutPipe, err := sess.StdoutPipe()
	if err != nil {
		return "", 0, err
	}
	stderrPipe, err := sess.StderrPipe()
	if err != nil {
		return "", 0, err
	}

	if err := sess.Start(cmd); err != nil {
		return "", 0, err
	}

	var out bytes.Buffer
	_, _ = out.ReadFrom(stdoutPipe)
	_, _ = out.ReadFrom(stderrPipe)

	ec := 0
	if werr := sess.Wait(); werr != nil {
		if ee, ok := werr.(*ssh.ExitError); ok {
			ec = ee.ExitStatus()
		} else {
			return out.String(), 0, werr
		}
	}
	return out.String(), ec, nil
}
