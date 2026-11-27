package sim

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

// TestTelnetAuth_StrictRejectsWrongPassword pins the new default
// posture: a telnet listener with credentials and permissive=false
// rejects a session that supplies the wrong password.
func TestTelnetAuth_StrictRejectsWrongPassword(t *testing.T) {
	dev := NewDevice("dev-tel-1", "test-host", net.ParseIP("127.0.0.1"), "test-profile", 1, map[string]any{})
	if err := dev.StartTelnet(0, "admin", "polaris", false); err != nil {
		t.Fatalf("StartTelnet: %v", err)
	}
	t.Cleanup(dev.StopTelnet)

	out := telnetLogin(t, dev.telnetListener.Addr().String(), "admin", "wrong")
	if !strings.Contains(out, "auth rejected") {
		t.Errorf("expected 'auth rejected' in output, got %q", out)
	}
}

// TestTelnetAuth_StrictAcceptsCorrectPassword complements the
// rejection case: the matching user and password reach the prompt.
func TestTelnetAuth_StrictAcceptsCorrectPassword(t *testing.T) {
	dev := NewDevice("dev-tel-2", "test-host", net.ParseIP("127.0.0.1"), "test-profile", 1, map[string]any{})
	if err := dev.StartTelnet(0, "admin", "polaris", false); err != nil {
		t.Fatalf("StartTelnet: %v", err)
	}
	t.Cleanup(dev.StopTelnet)

	out := telnetLogin(t, dev.telnetListener.Addr().String(), "admin", "polaris")
	if strings.Contains(out, "auth rejected") {
		t.Errorf("expected to reach prompt, got auth-rejected output: %q", out)
	}
	if !strings.Contains(out, "test-host>") {
		t.Errorf("expected hostname prompt in output, got %q", out)
	}
}

// TestTelnetAuth_PermissiveAcceptsAnything pins the opt-in escape
// hatch: a permissive listener accepts any user/password.
func TestTelnetAuth_PermissiveAcceptsAnything(t *testing.T) {
	dev := NewDevice("dev-tel-3", "test-host", net.ParseIP("127.0.0.1"), "test-profile", 1, map[string]any{})
	if err := dev.StartTelnet(0, "admin", "polaris", true); err != nil {
		t.Fatalf("StartTelnet: %v", err)
	}
	t.Cleanup(dev.StopTelnet)

	out := telnetLogin(t, dev.telnetListener.Addr().String(), "anyone", "anything")
	if strings.Contains(out, "auth rejected") {
		t.Errorf("expected permissive login to succeed, got %q", out)
	}
	if !strings.Contains(out, "test-host>") {
		t.Errorf("expected hostname prompt in output, got %q", out)
	}
}

// TestTelnetAuth_EmptyPasswordFallsBackPermissive covers the
// compatibility fallback: an empty default_password without an
// explicit permissive flag still accepts anything (with a warning).
func TestTelnetAuth_EmptyPasswordFallsBackPermissive(t *testing.T) {
	dev := NewDevice("dev-tel-4", "test-host", net.ParseIP("127.0.0.1"), "test-profile", 1, map[string]any{})
	if err := dev.StartTelnet(0, "admin", "", false); err != nil {
		t.Fatalf("StartTelnet: %v", err)
	}
	t.Cleanup(dev.StopTelnet)

	out := telnetLogin(t, dev.telnetListener.Addr().String(), "anyone", "anything")
	if strings.Contains(out, "auth rejected") {
		t.Errorf("expected fallback login to succeed, got %q", out)
	}
}

// telnetLogin connects to the listener, walks the option negotiation
// and login/password prompts, then reads up to 1KB of follow-on bytes
// so the caller can assert on either "auth rejected" or the command
// prompt. The connection is closed after the read window expires.
func telnetLogin(t *testing.T, addr, user, pass string) string {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	rd := bufio.NewReader(telnetReader{r: conn})

	// Drain any bytes the server pushed before login: option
	// negotiation, banner, login prompt. We don't parse it strictly,
	// we just wait for "login:" to know the prompt is ready.
	if err := readUntil(rd, "login:"); err != nil {
		t.Fatalf("waiting for login prompt: %v", err)
	}
	if _, err := conn.Write([]byte(user + "\r\n")); err != nil {
		t.Fatalf("write user: %v", err)
	}
	if err := readUntil(rd, "Password:"); err != nil {
		t.Fatalf("waiting for password prompt: %v", err)
	}
	if _, err := conn.Write([]byte(pass + "\r\n")); err != nil {
		t.Fatalf("write password: %v", err)
	}

	// Read whatever the server emits next within the deadline. Either
	// "auth rejected" + close, or the command prompt.
	buf := make([]byte, 1024)
	var out strings.Builder
	for {
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, err := rd.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return out.String()
}

// readUntil reads from rd until the accumulated string contains marker
// or the underlying reader returns an error.
func readUntil(rd *bufio.Reader, marker string) error {
	var acc strings.Builder
	buf := make([]byte, 256)
	for {
		n, err := rd.Read(buf)
		if n > 0 {
			acc.Write(buf[:n])
			if strings.Contains(acc.String(), marker) {
				return nil
			}
		}
		if err != nil {
			return err
		}
	}
}
