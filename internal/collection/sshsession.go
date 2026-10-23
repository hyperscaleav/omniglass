package collection

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The held ssh session (#1032): the line protocol over an ssh shell (or exec)
// channel. Auth is the transport's own step (#1049): userauth with the
// endpoint's credential (a password, a private key, or keyboard-interactive
// answered by the driver's login dialog), after the host key is checked
// against its pin. Both refusals are classified terminal: a refused
// credential is auth, a changed host key is host-key. The channel is bridged
// onto an in-memory pipe so the line reader keeps the read deadlines it
// draws every ask's boundary with.

// NewSSHSessionDialer returns the held ssh dialer.
func NewSSHSessionDialer() StagedDialer { return sshSessionDialer{} }

type sshSessionDialer struct{}

// Dial refuses: an ssh session always runs its auth step (Connect, then
// Authenticate), so a session with no credential declared cannot open one.
func (sshSessionDialer) Dial(context.Context, string, time.Duration, Inbound) (HeldConn, error) {
	return nil, &TerminalError{Class: driver.ClassProtocol, Reason: "an ssh session needs its credential, and none is declared"}
}

func (sshSessionDialer) Connect(ctx context.Context, target string, timeout time.Duration) (PendingConn, error) {
	if timeout <= 0 {
		timeout = defaultSessionTimeout
	}
	d := net.Dialer{Timeout: timeout, KeepAlive: SessionKeepAlive}
	c, err := d.DialContext(ctx, "tcp", target)
	if err != nil {
		return nil, fmt.Errorf("collection: ssh session dial %s: %w", target, err)
	}
	return &pendingSSH{c: c, target: target}, nil
}

type pendingSSH struct {
	c      net.Conn
	target string
	client *ssh.Client
}

// hostKeyChanged is the host key callback's refusal: the key offered is not
// the key pinned.
type hostKeyChanged struct{ pinned, offered string }

func (e *hostKeyChanged) Error() string { return "host key changed" }

// unansweredQuestion is a keyboard-interactive question the declared
// credential cannot answer. Its text is the server's, never a credential.
type unansweredQuestion struct{ err error }

func (e *unansweredQuestion) Error() string { return e.err.Error() }

// passwordQuestion is the keyboard-interactive question a password answers
// when the driver declares no login dialog.
var passwordQuestion = regexp.MustCompile(`(?i)password`)

func (p *pendingSSH) Authenticate(ctx context.Context, auth *SessionAuth, _ Classifier, timeout time.Duration) error {
	if auth == nil || auth.SSH == nil {
		return &TerminalError{Class: driver.ClassProtocol, Reason: "an ssh session needs its credential, and none is declared"}
	}
	if timeout <= 0 {
		timeout = defaultSessionTimeout
	}
	cred := auth.SSH
	var methods []ssh.AuthMethod
	if cred.PrivateKey != "" {
		signer, err := ssh.ParsePrivateKey([]byte(cred.PrivateKey))
		if err != nil {
			// Never the key or the parser's text, which may quote it.
			return &TerminalError{Class: driver.ClassAuth, Reason: "the endpoint's private key does not parse"}
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if cred.Password != "" {
		methods = append(methods, ssh.Password(cred.Password))
	}
	switch {
	case auth.Login != nil:
		methods = append(methods, ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
			answers, err := auth.Login.Answer(questions)
			if err != nil {
				return nil, &unansweredQuestion{err}
			}
			return answers, nil
		}))
	case cred.Password != "":
		methods = append(methods, ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
			answers := make([]string, 0, len(questions))
			for _, q := range questions {
				if !passwordQuestion.MatchString(q) {
					return nil, &unansweredQuestion{fmt.Errorf("the server asked %q, which a password does not answer", q)}
				}
				answers = append(answers, cred.Password)
			}
			return answers, nil
		}))
	}
	if len(methods) == 0 {
		return &TerminalError{Class: driver.ClassAuth, Reason: "the endpoint's credential carries no password, key, or login"}
	}
	offered := ""
	config := &ssh.ClientConfig{
		User: cred.Username,
		Auth: methods,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			offered = ssh.FingerprintSHA256(key)
			if cred.Pin != nil {
				if pinned := cred.Pin.Get(); pinned != "" && pinned != offered {
					return &hostKeyChanged{pinned: pinned, offered: offered}
				}
			}
			return nil
		},
		Timeout: timeout,
	}
	_ = p.c.SetDeadline(time.Now().Add(timeout))
	stop := context.AfterFunc(ctx, func() { _ = p.c.SetDeadline(time.Now()) })
	defer stop()
	cc, chans, reqs, err := ssh.NewClientConn(p.c, p.target, config)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var changed *hostKeyChanged
		if errors.As(err, &changed) {
			return &TerminalError{Class: driver.ClassHostKey, Reason: fmt.Sprintf(
				"the host key changed: pinned %s, offered %s; retry the endpoint with the offered key to trust it", changed.pinned, changed.offered)}
		}
		var unanswered *unansweredQuestion
		if errors.As(err, &unanswered) {
			// The server wants something the driver never declared (a
			// one-time code): no retry can answer it.
			return &TerminalError{Class: driver.ClassAuth, Reason: unanswered.err.Error()}
		}
		if strings.Contains(err.Error(), "unable to authenticate") {
			return &TerminalError{Class: driver.ClassAuth, Reason: "the component refused the credential: " + strings.TrimPrefix(err.Error(), "ssh: handshake failed: ")}
		}
		return fmt.Errorf("collection: ssh session %s: %w", p.target, err)
	}
	_ = p.c.SetDeadline(time.Time{})
	p.client = ssh.NewClient(cc, chans, reqs)
	if cred.Pin != nil {
		cred.Pin.Trust(offered)
	}
	return nil
}

// Open opens the line channel (a shell, with a PTY when asked, or an exec)
// and starts the line reader over it.
func (p *pendingSSH) Open(in Inbound, ch ChannelOptions) (HeldConn, error) {
	if p.client == nil {
		return nil, &TerminalError{Class: driver.ClassProtocol, Reason: "the ssh session opened before its auth step"}
	}
	sess, err := p.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("collection: ssh session %s: open channel: %w", p.target, err)
	}
	if ch.PTY {
		if err := sess.RequestPty("vt100", 24, 200, ssh.TerminalModes{ssh.ECHO: 0}); err != nil {
			_ = sess.Close()
			return nil, fmt.Errorf("collection: ssh session %s: pty: %w", p.target, err)
		}
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("collection: ssh session %s: %w", p.target, err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("collection: ssh session %s: %w", p.target, err)
	}
	sess.Stderr = io.Discard
	if ch.Exec != "" {
		err = sess.Start(ch.Exec)
	} else {
		err = sess.Shell()
	}
	if err != nil {
		_ = sess.Close()
		// A server that refuses the channel the driver declared will refuse
		// it on every attempt.
		return nil, &TerminalError{Class: driver.ClassProtocol, Reason: "the component refused the declared channel: " + err.Error()}
	}
	// Bridge the channel onto a pipe whose near end keeps deadlines.
	near, far := net.Pipe()
	client := p.client
	go func() {
		_, _ = io.Copy(far, stdout)
		_ = far.Close()
	}()
	go func() {
		_, _ = io.Copy(stdin, far)
		_ = stdin.Close()
	}()
	conn := &sshChannelConn{Conn: near, close: func() {
		_ = sess.Close()
		_ = client.Close()
	}}
	return openLineConn(conn, p.target, in), nil
}

func (p *pendingSSH) Close() error {
	if p.client != nil {
		return p.client.Close()
	}
	return p.c.Close()
}

// sshChannelConn is the near end of the channel's pipe; closing it ends the
// channel and the ssh connection.
type sshChannelConn struct {
	net.Conn
	close func()
}

func (c *sshChannelConn) Close() error {
	err := c.Conn.Close()
	c.close()
	return err
}
