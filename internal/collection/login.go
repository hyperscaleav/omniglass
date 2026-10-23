package collection

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The login dialog (#1049): one primitive for every prompt-and-answer login
// a session runs as its auth step. Over a byte stream (a raw tcp password
// prompt, a telnet login) RunLogin reads what the device writes, answers
// each prompt in order as it appears (a prompt need not end a line), and
// passes when the ready match shows. Over ssh keyboard-interactive the same
// dialog answers the server's questions (Answer). A credential is only ever
// written to the wire: never to an error, a reason or a log line.

// LoginPrompt is one rendered step: the prompt to wait for and the line to
// send (the secret already substituted, so it is never printed).
type LoginPrompt struct {
	Expect driver.Match
	Send   string
}

// LoginDialog is a driver's login with its sends rendered against the
// endpoint's delivered secrets (driver.Login.Render).
type LoginDialog struct {
	Steps []LoginPrompt
	Ready driver.Match
}

// String never prints a send.
func (d *LoginDialog) String() string {
	return fmt.Sprintf("login dialog (%d steps)", len(d.Steps))
}

// errLoginFailed is the cause of a login that neither passed nor was refused
// in time, or that the connection ended under: retryable, since a booting or
// briefly unreachable device says nothing about the credential.
var errLoginFailed = errors.New("login did not complete")

// RunLogin runs the dialog over conn before anything else reads it. Each
// wait (a prompt, or ready after the last answer) has timeout. A line the
// classifier names terminal, or a prompt for a step already answered (the
// device asking again: the credential was refused), fails with a
// TerminalError carrying the device's text; silence or a dropped connection
// fails retryably. Bytes after the ready match are left unread on conn,
// except those already read with it, which are dropped like any pre-ask
// banner.
func RunLogin(ctx context.Context, conn net.Conn, d *LoginDialog, terminal Classifier, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = defaultSessionTimeout
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetReadDeadline(time.Now()) })
	defer stop()
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()

	next := 0
	var partial strings.Builder
	judged := false // the partial line was judged after the component paused on it
	last := ""
	buf := make([]byte, 4096)
	deadline := time.Now().Add(timeout)
	// check judges one piece of text: a whole line, or the partial line a
	// prompt sits on. It returns done when the dialog passed. Every text that
	// can reach an error or a reason is scrubbed of the sends first, so a
	// component that echoes what it is sent never puts a credential there.
	check := func(text string) (done bool, err error) {
		if text == "" {
			return false, nil
		}
		clean := d.scrub(text)
		last = clean
		if terr := terminal.classify(text); terr != nil {
			terr.Reason = clean
			return false, terr
		}
		if next == len(d.Steps) && d.Ready.Matches(text) {
			return true, nil
		}
		for i := 0; i < next; i++ {
			if d.Steps[i].Expect.Matches(text) {
				return false, &TerminalError{Class: driver.ClassAuth, Reason: "the component prompted again: " + clean}
			}
		}
		if next < len(d.Steps) && d.Steps[next].Expect.Matches(text) {
			_ = conn.SetWriteDeadline(time.Now().Add(timeout))
			if _, err := conn.Write([]byte(d.Steps[next].Send + "\r\n")); err != nil {
				return false, fmt.Errorf("%w: answering step %d: %v", errLoginFailed, next+1, err)
			}
			next++
			partial.Reset()
			deadline = time.Now().Add(timeout)
		}
		return false, nil
	}
	for {
		// A partial line is judged only once the component pauses on it (a
		// prompt waits for its answer), never at whatever byte boundary a
		// read returns, so a banner arriving in pieces is read as a line.
		readBy := deadline
		if partial.Len() > 0 && !judged {
			if q := time.Now().Add(loginQuietWindow); q.Before(readBy) {
				readBy = q
			}
		}
		_ = conn.SetReadDeadline(readBy)
		n, rerr := conn.Read(buf)
		if n > 0 {
			judged = false
			chunk := strings.ReplaceAll(string(buf[:n]), "\r", "")
			for {
				i := strings.IndexByte(chunk, '\n')
				if i < 0 {
					break
				}
				partial.WriteString(chunk[:i])
				line := partial.String()
				partial.Reset()
				chunk = chunk[i+1:]
				if done, err := check(line); done || err != nil {
					return err
				}
			}
			partial.WriteString(chunk)
			if partial.Len() > maxLineAnswer {
				return fmt.Errorf("%w: a line exceeded %d bytes", errLoginFailed, maxLineAnswer)
			}
		}
		if rerr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			paused := errors.Is(rerr, os.ErrDeadlineExceeded) && time.Now().Before(deadline)
			if partial.Len() > 0 && !judged {
				// The component paused on a partial line, or hung up after
				// one ("auth rejected" with no line ending): judge it.
				judged = true
				if done, err := check(partial.String()); done || err != nil {
					return err
				}
				if paused {
					continue
				}
			} else if paused {
				continue
			}
			waiting := "ready"
			if next < len(d.Steps) {
				waiting = fmt.Sprintf("step %d's prompt", next+1)
			}
			if errors.Is(rerr, os.ErrDeadlineExceeded) {
				return fmt.Errorf("%w: no %s within %s (last: %q)", errLoginFailed, waiting, timeout, last)
			}
			return fmt.Errorf("%w: the connection ended waiting for %s (last: %q): %v", errLoginFailed, waiting, last, rerr)
		}
	}
}

// loginQuietWindow is how long the component must pause on a partial line
// before the dialog judges it as a prompt.
const loginQuietWindow = 100 * time.Millisecond

// scrub replaces every send in text with a mask: what the dialog writes is
// a credential, and a component that echoes it must not carry it into an
// error, a reason or a log line.
func (d *LoginDialog) scrub(text string) string {
	for _, st := range d.Steps {
		if st.Send != "" {
			text = strings.ReplaceAll(text, st.Send, "[redacted]")
		}
	}
	return text
}

// Answer answers keyboard-interactive questions: each is answered by the
// first step whose prompt matches it. A question no step expects refuses,
// naming it, so a server asking for something the driver never declared
// (a one-time code) fails the auth step rather than being sent a guess.
func (d *LoginDialog) Answer(questions []string) ([]string, error) {
	out := make([]string, 0, len(questions))
	for _, q := range questions {
		answered := false
		for _, st := range d.Steps {
			if st.Expect.Matches(q) {
				out = append(out, st.Send)
				answered = true
				break
			}
		}
		if !answered {
			return nil, fmt.Errorf("the server asked %q, which the login declares no answer for", q)
		}
	}
	return out, nil
}
