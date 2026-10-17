package sim

import (
	"bufio"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"golang.org/x/crypto/ssh"
)

// newPTPDevice builds a minimal device that exposes a persistent SSH
// shell with PTP-style verbs over the loopback interface. Used by the
// shellsession tests to exercise the full open → subscribe →
// state-mutation → push round-trip without an agent or server.
func newPTPDevice(t *testing.T, hostname string) (*Device, *eventCollector) {
	t.Helper()
	dev := NewDevice("ptp-"+hostname, hostname, net.ParseIP("127.0.0.1"), "polaris-dsp-ptp", 1,
		map[string]any{
			"status": map[string]any{
				"channels": []any{
					map[string]any{"index": 0, "name": "Mixer1", "level": -12.5, "mute": false},
					map[string]any{"index": 1, "name": "Mixer2", "level": -6.0, "mute": false},
				},
			},
		})

	mustAdd := func(c *profile.Command) {
		t.Helper()
		response := c.Response
		if response == "" {
			response = "+OK"
		}
		c.Response = response
		if err := dev.AddCommandFromProfile(c, response); err != nil {
			t.Fatalf("AddCommand %q: %v", c.Match, err)
		}
	}

	// Reads
	mustAdd(&profile.Command{
		Match:    "Mixer1 get level",
		Response: `+OK "value":-12.5`,
	})
	// Writes, drive state mutation from inside the session for one
	// of the test scenarios.
	mustAdd(&profile.Command{
		Match:    "Mixer1 set level -6",
		Response: "+OK",
		Mutate: map[string]any{
			"status.channels.0.level": "-6",
		},
	})
	// On-change subscribe / unsubscribe to Mixer1.level.
	mustAdd(&profile.Command{
		Match:    "Mixer1 subscribe level",
		Action:   "subscribe",
		Subject:  "status.channels.0.level",
		Response: "+OK",
	})
	mustAdd(&profile.Command{
		Match:    "Mixer1 unsubscribe level",
		Action:   "unsubscribe",
		Subject:  "status.channels.0.level",
		Response: "+OK",
	})
	// Tokenized subscribe.
	mustAdd(&profile.Command{
		Match:    "Mixer1 subscribe level bgMeter1",
		Action:   "subscribe",
		Subject:  "status.channels.0.level",
		Token:    "bgMeter1",
		Response: "+OK",
	})
	// Interval subscribe, 100ms is well above the default
	// publish_min_interval_ms floor (50).
	mustAdd(&profile.Command{
		Match:      "Mixer1 subscribe level 100",
		Action:     "subscribe",
		Subject:    "status.channels.0.level",
		IntervalMs: "100",
		Response:   "+OK",
	})

	iface := &profile.Interface{
		Name:       "ptp",
		Protocol:   "ssh",
		Port:       0,
		Persistent: true,
		LineEnding: "\r\n",
		Welcome:    "Welcome to PTP",
		Subjects: []profile.Subject{
			{
				Match:        "status.channels.*.level",
				Path:         "{{ .Subject }}",
				PublishToken: "level",
				PushFormat:   `! "publishToken":"{{ .Token }}" "subject":"{{ .Subject }}" "value":{{ .Value }}`,
			},
		},
	}
	cfg, err := CompileSessionConfig(iface)
	if err != nil {
		t.Fatalf("CompileSessionConfig: %v", err)
	}
	collector := newEventCollector()
	reg := NewSessionRegistry(dev, cfg, collector.publish)
	dev.AttachSSHSession(reg)
	return dev, collector
}

type eventCollector struct {
	mu     sync.Mutex
	events []SessionEvent
}

func newEventCollector() *eventCollector { return &eventCollector{} }

func (c *eventCollector) publish(e SessionEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *eventCollector) snapshot() []SessionEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]SessionEvent, len(c.events))
	copy(out, c.events)
	return out
}

func (c *eventCollector) waitForKind(t *testing.T, kind string, timeout time.Duration) SessionEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, e := range c.snapshot() {
			if e.Kind == kind {
				return e
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for session event kind=%q (got %+v)", kind, c.snapshot())
	return SessionEvent{}
}

// dialPTPShell dials the device's SSH listener, opens a shell channel,
// and returns stdin / stdout pipes plus a teardown helper. The shell
// channel stays open until the caller closes the returned client.
func dialPTPShell(t *testing.T, addr string) (io.WriteCloser, *bufio.Reader, *ssh.Session, *ssh.Client) {
	t.Helper()
	cfg := &ssh.ClientConfig{
		User:            "admin",
		Auth:            []ssh.AuthMethod{ssh.Password("admin")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		t.Fatalf("ssh dial: %v", err)
	}

	sess, err := client.NewSession()
	if err != nil {
		client.Close()
		t.Fatalf("NewSession: %v", err)
	}

	stdin, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		client.Close()
		t.Fatalf("StdinPipe: %v", err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		client.Close()
		t.Fatalf("StdoutPipe: %v", err)
	}

	if err := sess.Shell(); err != nil {
		sess.Close()
		client.Close()
		t.Fatalf("Shell: %v", err)
	}

	return stdin, bufio.NewReader(stdout), sess, client
}

// readShellLineMatching reads lines from r until one matches pred, with a deadline.
// Returns the matching line.
func readShellLineMatching(t *testing.T, r *bufio.Reader, pred func(string) bool, timeout time.Duration) string {
	t.Helper()
	type result struct {
		line string
		err  error
	}
	out := make(chan result, 1)
	go func() {
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				out <- result{line, err}
				return
			}
			if pred(line) {
				out <- result{line, nil}
				return
			}
		}
	}()
	select {
	case res := <-out:
		if res.err != nil {
			t.Fatalf("readShellLineMatching: %v (line so far: %q)", res.err, res.line)
		}
		return res.line
	case <-time.After(timeout):
		t.Fatalf("readShellLineMatching timed out after %s", timeout)
		return ""
	}
}

// TestPersistentSession_OnChangePush opens a real SSH shell, registers
// an on-change subscription, mutates the underlying state via the
// device's mutate machinery, and asserts the push line arrives on the
// session within a tight timeout. This is the end-to-end protocol
// proof: if this passes, the registry, fanout, shellsession loop, and
// SSH wiring all line up.
func TestPersistentSession_OnChangePush(t *testing.T) {
	dev, collector := newPTPDevice(t, "polaris-onchg")
	if err := dev.StartSSH(0, "admin", "admin", false); err != nil {
		t.Fatalf("StartSSH: %v", err)
	}
	t.Cleanup(dev.StopSSH)

	stdin, stdout, sess, client := dialPTPShell(t, dev.sshListener.Addr().String())
	defer client.Close()
	defer sess.Close()

	// Welcome banner arrives first.
	readShellLineMatching(t, stdout, func(s string) bool {
		return strings.Contains(s, "Welcome to PTP")
	}, 2*time.Second)

	// Subscribe.
	if _, err := io.WriteString(stdin, "Mixer1 subscribe level\r\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	readShellLineMatching(t, stdout, func(s string) bool {
		return strings.HasPrefix(s, "+OK")
	}, 2*time.Second)

	// Confirm session_open lifecycle event was published.
	collector.waitForKind(t, "open", 2*time.Second)

	// Mutate state from outside the session, same path the HTTP
	// route would take. This calls applyMutations and triggers
	// SessionRegistry.Fanout.
	if _, ok, err := dev.RunCommand("Mixer1 set level -6"); err != nil || !ok {
		t.Fatalf("mutate via RunCommand: ok=%v err=%v", ok, err)
	}

	// The subscribe response above already returned. The mutation
	// from RunCommand fires Fanout under the device lock, which
	// queues a push line onto the session's Out channel. The
	// shellsession output pump drains Out → SSH stdout.
	push := readShellLineMatching(t, stdout, func(s string) bool {
		return strings.HasPrefix(s, "!")
	}, 2*time.Second)
	if !strings.Contains(push, `"subject":"status.channels.0.level"`) {
		t.Errorf("push missing subject: %q", push)
	}
	if !strings.Contains(push, `"value":-6`) {
		t.Errorf("push missing value: %q", push)
	}

	// Also confirm the upstream push event was emitted.
	collector.waitForKind(t, "push", 2*time.Second)
}

// TestPersistentSession_IntervalPush asserts the heartbeat / interval
// pump fires on its cadence even without a state change.
func TestPersistentSession_IntervalPush(t *testing.T) {
	dev, _ := newPTPDevice(t, "polaris-interval")
	if err := dev.StartSSH(0, "admin", "admin", false); err != nil {
		t.Fatalf("StartSSH: %v", err)
	}
	t.Cleanup(dev.StopSSH)

	stdin, stdout, sess, client := dialPTPShell(t, dev.sshListener.Addr().String())
	defer client.Close()
	defer sess.Close()

	readShellLineMatching(t, stdout, func(s string) bool {
		return strings.Contains(s, "Welcome to PTP")
	}, 2*time.Second)

	if _, err := io.WriteString(stdin, "Mixer1 subscribe level 100\r\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	readShellLineMatching(t, stdout, func(s string) bool {
		return strings.HasPrefix(s, "+OK")
	}, 2*time.Second)

	// Interval pump should emit at ~100ms. Wait for at least two
	// pushes in under 1 second to confirm the cadence is real and
	// not single-shot.
	count := 0
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) && count < 2 {
		readShellLineMatching(t, stdout, func(s string) bool {
			if strings.HasPrefix(s, "!") {
				count++
				return true
			}
			return false
		}, 500*time.Millisecond)
	}
	if count < 2 {
		t.Errorf("interval push count = %d, want >= 2", count)
	}
}

// TestPersistentSession_TokenOverride asserts the per-command token
// template overrides the Subject's default PublishToken.
func TestPersistentSession_TokenOverride(t *testing.T) {
	dev, _ := newPTPDevice(t, "polaris-token")
	if err := dev.StartSSH(0, "admin", "admin", false); err != nil {
		t.Fatalf("StartSSH: %v", err)
	}
	t.Cleanup(dev.StopSSH)

	stdin, stdout, sess, client := dialPTPShell(t, dev.sshListener.Addr().String())
	defer client.Close()
	defer sess.Close()

	readShellLineMatching(t, stdout, func(s string) bool {
		return strings.Contains(s, "Welcome to PTP")
	}, 2*time.Second)

	if _, err := io.WriteString(stdin, "Mixer1 subscribe level bgMeter1\r\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	readShellLineMatching(t, stdout, func(s string) bool {
		return strings.HasPrefix(s, "+OK")
	}, 2*time.Second)

	// Mutate to fire on-change push.
	if _, ok, err := dev.RunCommand("Mixer1 set level -6"); err != nil || !ok {
		t.Fatalf("mutate: ok=%v err=%v", ok, err)
	}

	push := readShellLineMatching(t, stdout, func(s string) bool {
		return strings.HasPrefix(s, "!")
	}, 2*time.Second)
	if !strings.Contains(push, `"publishToken":"bgMeter1"`) {
		t.Errorf("push missing custom token: %q", push)
	}
}

// TestPersistentSession_ExitClosesSession asserts that typing `exit`
// (or `bye` / `quit`) actually terminates the persistent shell.
// Regression for the case where the output pump's `range sess.Out`
// blocked forever because Close() doesn't close the channel, the
// shellsession loop never returned, the SSH channel never closed,
// and the client side hung indefinitely.
func TestPersistentSession_ExitClosesSession(t *testing.T) {
	for _, cmd := range []string{"exit", "bye", "quit"} {
		t.Run(cmd, func(t *testing.T) {
			dev, _ := newPTPDevice(t, "polaris-exit-"+cmd)
			if err := dev.StartSSH(0, "admin", "admin", false); err != nil {
				t.Fatalf("StartSSH: %v", err)
			}
			t.Cleanup(dev.StopSSH)

			stdin, stdout, sess, client := dialPTPShell(t, dev.sshListener.Addr().String())
			defer client.Close()
			defer sess.Close()

			readShellLineMatching(t, stdout, func(s string) bool {
				return strings.Contains(s, "Welcome to PTP")
			}, 2*time.Second)

			if _, err := io.WriteString(stdin, cmd+"\r\n"); err != nil {
				t.Fatalf("write: %v", err)
			}

			// Server writes "bye<CRLF>" then closes the channel.
			readShellLineMatching(t, stdout, func(s string) bool {
				return strings.HasPrefix(s, "bye")
			}, 2*time.Second)

			// sess.Wait must return; if it hangs, runShellSession
			// never finished and the bug regressed.
			done := make(chan error, 1)
			go func() { done <- sess.Wait() }()
			select {
			case <-done:
				// expected, channel closed cleanly
			case <-time.After(2 * time.Second):
				t.Fatalf("ssh session.Wait did not return after %q, shell loop hung", cmd)
			}
		})
	}
}

// TestSessionRegistry_SetBusLateBinding pins the regression where
// engine.SetSessionPublisher was called AFTER engine.LoadDevices
// (the order a run-mode caller may use), so registries captured a nil
// bus at construction time and silently dropped every SessionEvent
// for the process lifetime, meaning the dashboard tile and chip
// badges had nothing to react to. The fix wires SetBus on existing
// registries; this test asserts a nil-built registry, after a later
// SetBus call, actually publishes events.
func TestSessionRegistry_SetBusLateBinding(t *testing.T) {
	dev := NewDevice("late-bind-1", "late-bind-host", net.ParseIP("127.0.0.1"), "test", 1, map[string]any{})
	cfg, err := CompileSessionConfig(&profile.Interface{
		Name:       "ssh",
		Protocol:   "ssh",
		Port:       22,
		Persistent: true,
		LineEnding: "\r\n",
		Subjects: []profile.Subject{{
			Match: "*.*", Path: "{{ .Subject }}", PushFormat: "ok",
		}},
	})
	if err != nil {
		t.Fatalf("CompileSessionConfig: %v", err)
	}
	// Construct registry with nil bus, mirrors the real-world race
	// where SetSessionPublisher hasn't been wired yet.
	reg := NewSessionRegistry(dev, cfg, nil)

	// Open before bus is wired, must not crash, must not publish.
	sess1, err := reg.Open("client1")
	if err != nil {
		t.Fatalf("Open before bus: %v", err)
	}
	reg.Close(sess1)

	// Now late-bind the bus.
	collector := newEventCollector()
	reg.SetBus(collector.publish)

	// Subsequent open/close must publish through the late-bound bus.
	sess2, err := reg.Open("client2")
	if err != nil {
		t.Fatalf("Open after SetBus: %v", err)
	}
	reg.Close(sess2)

	events := collector.snapshot()
	if len(events) != 2 {
		t.Fatalf("expected 2 events (open + close after SetBus); got %d: %+v", len(events), events)
	}
	if events[0].Kind != "open" || events[1].Kind != "close" {
		t.Errorf("expected [open, close]; got [%s, %s]", events[0].Kind, events[1].Kind)
	}
	if events[0].SessionID != sess2.ID || events[1].SessionID != sess2.ID {
		t.Errorf("events should reference sess2.ID=%q; got open=%q close=%q",
			sess2.ID, events[0].SessionID, events[1].SessionID)
	}
}

// TestPersistentSession_HelpListsCommands asserts the built-in `help`
// verb prints the registered command set without needing any
// profile-defined entry. This is the contract that lets a fresh
// operator type `help` and discover what the device supports.
func TestPersistentSession_HelpListsCommands(t *testing.T) {
	dev, _ := newPTPDevice(t, "polaris-help")
	if err := dev.StartSSH(0, "admin", "admin", false); err != nil {
		t.Fatalf("StartSSH: %v", err)
	}
	t.Cleanup(dev.StopSSH)

	stdin, stdout, sess, client := dialPTPShell(t, dev.sshListener.Addr().String())
	defer client.Close()
	defer sess.Close()

	readShellLineMatching(t, stdout, func(s string) bool {
		return strings.Contains(s, "Welcome to PTP")
	}, 2*time.Second)

	if _, err := io.WriteString(stdin, "help\r\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Expect the preamble first; then somewhere in the next several
	// lines, every command we registered in newPTPDevice should show
	// up. Drain a generous number of lines and join them.
	combined := readShellLineMatching(t, stdout, func(s string) bool {
		return strings.Contains(s, "Available commands:")
	}, 2*time.Second)
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) && (!strings.Contains(combined, "Mixer1 subscribe level") ||
		!strings.Contains(combined, "Mixer1 unsubscribe level") ||
		!strings.Contains(combined, "Mixer1 set level -6") ||
		!strings.Contains(combined, "Shell built-ins") ||
		!strings.Contains(combined, "bye, quit, exit")) {
		combined += readShellLineMatching(t, stdout, func(_ string) bool { return true }, 500*time.Millisecond)
	}
	for _, want := range []string{
		"Mixer1 get level",
		"Mixer1 set level -6",
		"Mixer1 subscribe level",
		"Mixer1 unsubscribe level",
		"Shell built-ins",
		"help, ?",
		"bye, quit, exit",
	} {
		if !strings.Contains(combined, want) {
			t.Errorf("help output missing %q; got:\n%s", want, combined)
		}
	}
}

// TestPersistentSession_Unsubscribe asserts that after UNSUBSCRIBE no
// further pushes arrive on the session, even when state changes again.
func TestPersistentSession_Unsubscribe(t *testing.T) {
	dev, _ := newPTPDevice(t, "polaris-unsub")
	if err := dev.StartSSH(0, "admin", "admin", false); err != nil {
		t.Fatalf("StartSSH: %v", err)
	}
	t.Cleanup(dev.StopSSH)

	stdin, stdout, sess, client := dialPTPShell(t, dev.sshListener.Addr().String())
	defer client.Close()
	defer sess.Close()

	readShellLineMatching(t, stdout, func(s string) bool {
		return strings.Contains(s, "Welcome to PTP")
	}, 2*time.Second)

	mustWrite := func(line string) {
		if _, err := io.WriteString(stdin, line); err != nil {
			t.Fatalf("write %q: %v", line, err)
		}
	}

	mustWrite("Mixer1 subscribe level\r\n")
	readShellLineMatching(t, stdout, func(s string) bool { return strings.HasPrefix(s, "+OK") }, 2*time.Second)

	mustWrite("Mixer1 unsubscribe level\r\n")
	readShellLineMatching(t, stdout, func(s string) bool { return strings.HasPrefix(s, "+OK") }, 2*time.Second)

	// Mutate; expect NO push line (we already consumed both +OKs).
	if _, ok, err := dev.RunCommand("Mixer1 set level -6"); err != nil || !ok {
		t.Fatalf("mutate: ok=%v err=%v", ok, err)
	}

	// Set a short read deadline by doing a non-blocking peek using a
	// separate goroutine + timeout. If a push arrives within 500ms we
	// fail; otherwise we pass.
	got := make(chan string, 1)
	go func() {
		line, err := stdout.ReadString('\n')
		if err == nil {
			got <- line
		}
	}()
	select {
	case line := <-got:
		t.Errorf("got unexpected push after unsubscribe: %q", line)
	case <-time.After(500 * time.Millisecond):
		// expected, no push
	}
}
