package sim

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
)

// runShellSession is the bidirectional command/push loop shared by the
// SSH shell channel handler and the Telnet persistent-mode handler.
// It reads lines from rd, dispatches each through Device.RunCommand,
// writes responses to w, and concurrently drains the session's Out
// channel of subscription push lines onto w. The loop returns when
// the reader hits EOF, the session closes, or readDeadline fires
// without input (handled by the caller's deadline plumbing, this
// function does not own connection deadlines).
//
// The caller owns:
//   - opening / closing the session (registry.Open + registry.Close)
//   - writing the welcome banner before calling
//   - line discipline on the reader (raw bytes vs telnet-IAC-stripped)
//   - the deadline strategy on the underlying conn
//
// Because both the inbound dispatch and the outbound pump share the
// same writer, runShellSession serializes writes through a single
// goroutine: the main loop reads lines and queues responses through
// the same Out channel that subscriptions use. Push lines and
// command responses thus interleave naturally without a separate
// write mutex.
func runShellSession(d *Device, sess *Session, rd *bufio.Reader, w io.Writer, port int, protocol string) {
	cfg := sess.registry.cfg
	if cfg.Welcome != "" {
		// Welcome is operator-facing greeting text, NOT a protocol
		// frame: skip FrameStart/FrameEnd brackets but do append
		// LineEnding so the next inbound read aligns to the
		// configured terminator.
		_, _ = io.WriteString(w, cfg.Welcome)
		_, _ = io.WriteString(w, cfg.LineEnding)
	}

	// Output pump: drains Out into w. Exits when the session closes
	// or w returns an error.
	//
	// Close(sess) signals via sess.Closed() but deliberately does NOT
	// close sess.Out, concurrent publish() / queueLine senders use
	// non-blocking selects on Out and would panic (send-on-closed)
	// if we closed it from under them. Instead the pump observes
	// Closed(), then drains any buffered lines best-effort before
	// exiting. Using `range Out` here would hang forever because
	// nothing ever closes Out.
	pumpDone := make(chan struct{})
	go func() {
		defer close(pumpDone)
		for {
			select {
			case line := <-sess.Out:
				if _, err := w.Write(line); err != nil {
					return
				}
			case <-sess.Closed():
				for {
					select {
					case line := <-sess.Out:
						_, _ = w.Write(line)
					default:
						return
					}
				}
			}
		}
	}()

	// Reader loop: read one frame per LineEnding-terminator, strip
	// the wire envelope (FrameStart / FrameEnd / checksum), dispatch
	// the resulting payload as a command, queue the response onto
	// Out so it interleaves with pushes through one writer.
	// ReadBytes returns io.EOF when the peer closes; that's a normal
	// session end. The terminator is the LAST byte of LineEnding,
	// for "\r\n" that's '\n' (and the read trims "\r\n"); for "\x00"
	// it's '\x00' (Q-SYS QRC); for "\r" it's '\r' (some legacy gear).
	delim := cfg.ReadDelim()
	for {
		select {
		case <-sess.Closed():
			<-pumpDone
			return
		default:
		}

		raw, err := rd.ReadBytes(delim)
		if err != nil {
			if err != io.EOF {
				slog.Debug("shell session read error",
					"device", d.Hostname, "session", sess.ID, "error", err)
			}
			sess.registry.Close(sess)
			<-pumpDone
			return
		}
		stripped := cfg.StripInbound(raw)
		// Trim leading/trailing whitespace ON TEXT-FLAVORED frames
		// only. For binary protocols (frame_start/end set, hex
		// matches), the payload may legitimately include high-bit
		// bytes that string-trim would corrupt. The conservative
		// rule: trim only when no FrameStart/FrameEnd is configured
		//, text protocols rely on the trim, binary protocols opt
		// out by declaring brackets.
		var cmd string
		if cfg.FrameStart == "" && cfg.FrameEnd == "" {
			cmd = strings.TrimSpace(string(stripped))
		} else {
			cmd = string(stripped)
		}
		if cmd == "" {
			continue
		}
		if cmd == "bye" || cmd == "quit" || cmd == "exit" {
			sess.queueLine([]byte(cfg.WrapOutbound("bye")))
			sess.registry.Close(sess)
			<-pumpDone
			return
		}

		// Built-in help. Always available; not driven by the
		// profile so every persistent device shows the same shape
		// of output regardless of vendor. Lists the registered
		// command match strings plus the persistent-shell
		// built-ins (subscribe verbs are profile-defined and
		// already appear in the command list).
		//
		// Help output is always CRLF-separated, even on listeners
		// whose wire LineEnding is a NUL (Q-SYS QRC) or other
		// non-newline byte. Help is operator-facing text rendered
		// in an xterm, readable line breaks matter more than
		// protocol-faithful framing. Real automated clients have
		// no reason to call `help`; it's a lab convenience.
		if cmd == "help" || cmd == "?" {
			lines := buildHelpLines(d, "\r\n")
			sess.queueLine([]byte(lines))
			continue
		}

		start := time.Now()
		result, ok, rerr := d.RunCommand(cmd)
		elapsed := time.Since(start)

		var status int
		switch {
		case !ok:
			sess.queueLine([]byte(cfg.WrapOutbound(unknownCommandLine(cmd))))
			status = 404
		case rerr != nil:
			sess.queueLine([]byte(cfg.WrapOutbound("-ERR " + rerr.Error())))
			status = 500
		default:
			if result.Response != "" {
				// Profile templates render the bare payload;
				// WrapOutbound adds FrameStart/FrameEnd/LineEnding.
				// Strip a trailing LineEnding the template may
				// have appended itself so we don't double up.
				payload := strings.TrimSuffix(result.Response, cfg.LineEnding)
				sess.queueLine([]byte(cfg.WrapOutbound(payload)))
			}
			status = 200

			// Apply subscribe / unsubscribe AFTER queueing the
			// response so the +OK lands on the wire before any push
			// the registration triggers (interval pumps emit their
			// first push immediately on the next tick; on-change
			// fires only on subsequent state mutations).
			if result.Action == ActionSubscribe {
				if subErr := sess.Subscribe(result.Subject, result.Token, result.IntervalMs); subErr != nil {
					sess.queueLine([]byte(cfg.WrapOutbound("-ERR " + subErr.Error())))
					status = 500
				}
			} else if result.Action == ActionUnsubscribe {
				sess.Unsubscribe(result.Subject)
			}
		}

		if d.Logger != nil {
			d.Logger(RequestLog{
				DeviceID:   d.DeviceID,
				Hostname:   d.Hostname,
				Protocol:   protocol,
				Port:       port,
				Method:     "EXEC",
				Path:       cmd,
				Remote:     sess.Remote,
				LocalAddr:  fmt.Sprintf("%s:%d", d.LoopbackIP.String(), port),
				Status:     status,
				DurationMs: elapsed.Milliseconds(),
			})
		}
	}
}

// buildHelpLines renders the auto-generated help output: a short
// preamble followed by every registered command on the device, then
// the shell built-ins. Plain text, no template, so every
// persistent device speaks the same help dialect regardless of
// profile-declared response style.
//
// Devices that declare both ssh and telnet interfaces register
// commands once per interface, so Device.CommandList returns
// duplicates whose pairing is opaque from the help layer's
// perspective. Dedup here while preserving first-seen order so the
// listing reads in the same order the operator would scan the YAML.
func buildHelpLines(d *Device, lineEnd string) string {
	seen := make(map[string]struct{})
	var b strings.Builder
	b.WriteString("Available commands:")
	b.WriteString(lineEnd)
	for _, m := range d.CommandList() {
		if _, dup := seen[m]; dup {
			continue
		}
		seen[m] = struct{}{}
		b.WriteString("  ")
		b.WriteString(m)
		b.WriteString(lineEnd)
	}
	b.WriteString(lineEnd)
	b.WriteString("Shell built-ins:")
	b.WriteString(lineEnd)
	b.WriteString("  help, ?      list available commands")
	b.WriteString(lineEnd)
	b.WriteString("  bye, quit, exit  close this session")
	b.WriteString(lineEnd)
	return b.String()
}

// queueLine enqueues a non-push line (command response, error message)
// onto the session's outbound channel. Same backpressure rule as
// publish: full buffer ⇒ close the session.
func (s *Session) queueLine(line []byte) {
	select {
	case s.Out <- line:
	case <-s.closed:
	default:
		go s.registry.Close(s)
	}
}

func unknownCommandLine(cmd string) string {
	return "-ERR unknown command: " + cmd
}
