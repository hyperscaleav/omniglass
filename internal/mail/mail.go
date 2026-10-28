// Package mail is the email half of the notification send (#1317, #1341):
// the message a recipient gets, built purely from a notification row (so a
// retry sends the same bytes), and the SMTP session that hands it to the
// install's relay. The session dials through the egress primitive's one dial
// (internal/egress), so the relay is judged by the same destination contract
// a webhook is: an internal address only when the allow list names it, a
// metadata address never. TLS is required (implicit, or STARTTLS the relay
// must offer) unless the relay is a host the allow list names and the
// install chose none, the lab's mail sim being the case; credentials are
// never sent over a connection that is not encrypted, unless it is loopback.
package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	netmail "net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hyperscaleav/omniglass/internal/egress"
)

// Relay security modes.
const (
	SecurityStartTLS = "starttls"
	SecurityTLS      = "tls"
	SecurityNone     = "none"
)

// Message is one email about one notification row.
type Message struct {
	// ID is the notification's uuid: the Message-ID's local part, the same on
	// every attempt, so a mailbox or a relay can recognise a redelivery.
	ID      string
	From    string
	To      []string
	Subject string
	Text    string
	// Date is the row's own time, not the attempt's, so every attempt
	// carries the same bytes.
	Date time.Time
}

// ErrMessage is a message that cannot be built: an address that does not
// parse, or a header value carrying a line break.
var ErrMessage = errors.New("mail: the message cannot be built")

// CheckAddress parses one address (a bare address or "Name <address>") and
// returns its address part.
func CheckAddress(s string) (string, error) {
	if strings.ContainsAny(s, "\r\n") {
		return "", fmt.Errorf("%w: an address carries a line break", ErrMessage)
	}
	a, err := netmail.ParseAddress(s)
	if err != nil {
		return "", fmt.Errorf("%w: %q is not an email address", ErrMessage, s)
	}
	return a.Address, nil
}

// Build renders m as an RFC 5322 message: plain text, UTF-8, quoted-printable,
// CRLF-delimited. The subject is a single line, encoded when not ASCII.
func Build(m Message) ([]byte, error) {
	if _, err := CheckAddress(m.From); err != nil {
		return nil, err
	}
	if len(m.To) == 0 {
		return nil, fmt.Errorf("%w: no recipient", ErrMessage)
	}
	for _, to := range m.To {
		if _, err := CheckAddress(to); err != nil {
			return nil, err
		}
	}
	if strings.ContainsAny(m.Subject, "\r\n") || strings.ContainsAny(m.ID, "\r\n<>@ ") {
		return nil, fmt.Errorf("%w: a header carries a line break", ErrMessage)
	}
	var b bytes.Buffer
	h := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	h("From", m.From)
	h("To", strings.Join(m.To, ", "))
	h("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	h("Date", m.Date.UTC().Format(time.RFC1123Z))
	h("Message-ID", "<"+m.ID+"@omniglass>")
	h("X-Omniglass-Notification", m.ID)
	h("Auto-Submitted", "auto-generated")
	h("MIME-Version", "1.0")
	h("Content-Type", "text/plain; charset=utf-8")
	h("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	w := quotedprintable.NewWriter(&b)
	if _, err := w.Write([]byte(strings.ReplaceAll(strings.ReplaceAll(m.Text, "\r\n", "\n"), "\n", "\r\n"))); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMessage, err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMessage, err)
	}
	b.WriteString("\r\n")
	return b.Bytes(), nil
}

// Relay is the install's mail relay: host:port, its security mode, and the
// credential it takes, if any (never printed).
type Relay struct {
	Addr     string
	Security string
	Username string
	Password string
}

// String never renders the credential.
func (r Relay) String() string { return "mail relay " + r.Addr }

// Result is what one send got back: the relay's reply code (0 when none
// came) and, for a failure, a reason fit to record.
type Result struct {
	OK     bool
	Status int
	Reason string
}

// Client sends through a relay under one egress policy.
type Client struct {
	policy egress.Policy
	opts   egress.Options
}

// New returns a client under policy; opts carries a test's roots and
// resolver, as the HTTP client's do.
func New(policy egress.Policy, opts egress.Options) *Client {
	return &Client{policy: policy, opts: opts}
}

// Send hands one built message to the relay for every recipient in to. A
// refused relay, a failed TLS handshake, a timeout and a relay that answers
// with an error are each a Result with OK false and a reason; a send is one
// bounded session (egress.TotalTimeout).
func (c *Client) Send(ctx context.Context, r Relay, from string, to []string, msg []byte) Result {
	host, _, err := net.SplitHostPort(r.Addr)
	if err != nil || host == "" {
		return Result{Reason: "the mail relay is not a host:port"}
	}
	switch r.Security {
	case SecurityStartTLS, SecurityTLS:
	case SecurityNone:
		if !c.policy.Names(host) {
			return Result{Reason: "a mail relay without TLS is only for a host the egress allow list names; use starttls or tls"}
		}
	default:
		return Result{Reason: fmt.Sprintf("mail_security %q is not starttls, tls or none", r.Security)}
	}
	ctx, cancel := context.WithTimeout(ctx, egress.TotalTimeout)
	defer cancel()
	conn, err := c.policy.DialContext(ctx, c.opts, "tcp", r.Addr)
	if err != nil {
		return Result{Reason: egress.Reason(err)}
	}
	defer conn.Close()
	dl, _ := ctx.Deadline()
	_ = conn.SetDeadline(dl)
	tlsCfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12, RootCAs: c.opts.RootCAs}
	if r.Security == SecurityTLS {
		tc := tls.Client(conn, tlsCfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			return Result{Reason: "TLS verification failed: " + err.Error()}
		}
		conn = tc
	}
	sc, err := smtp.NewClient(conn, host)
	if err != nil {
		return failed(err)
	}
	defer sc.Close()
	if err := sc.Hello("omniglass"); err != nil {
		return failed(err)
	}
	if r.Security == SecurityStartTLS {
		if ok, _ := sc.Extension("STARTTLS"); !ok {
			return Result{Reason: "the mail relay does not offer STARTTLS; set mail_security to tls, or to none for a relay the egress allow list names"}
		}
		if err := sc.StartTLS(tlsCfg); err != nil {
			return Result{Reason: "TLS verification failed: " + err.Error()}
		}
	}
	if r.Username != "" {
		// PlainAuth itself refuses an unencrypted session to anything but
		// loopback, so a credential never crosses the network in the clear.
		if err := sc.Auth(smtp.PlainAuth("", r.Username, r.Password, host)); err != nil {
			return failed(err)
		}
	}
	if err := sc.Mail(from); err != nil {
		return failed(err)
	}
	for _, rcpt := range to {
		if err := sc.Rcpt(rcpt); err != nil {
			return failed(err)
		}
	}
	w, err := sc.Data()
	if err != nil {
		return failed(err)
	}
	if _, err := w.Write(msg); err != nil {
		return failed(err)
	}
	if err := w.Close(); err != nil {
		return failed(err)
	}
	_ = sc.Quit()
	return Result{OK: true, Status: 250}
}

// failed words a session error: the relay's own reply with its code, or a
// transport failure without any credential.
func failed(err error) Result {
	var te *textproto.Error
	if errors.As(err, &te) {
		return Result{Status: te.Code, Reason: fmt.Sprintf("the mail relay answered %d: %s", te.Code, printable(te.Msg))}
	}
	if strings.Contains(err.Error(), "unencrypted connection") {
		return Result{Reason: "the mail relay's credential is only sent over TLS (or to loopback); set mail_security to starttls or tls"}
	}
	return Result{Reason: egress.Reason(err)}
}

// printable keeps a reply excerpt to one bounded line of printable text, fit
// for a text column: control characters (a NUL among them) dropped, invalid
// UTF-8 replaced, and the cut made on a rune boundary.
func printable(s string) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	var sb strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			sb.WriteByte(' ')
		case r >= 0x20 && r != 0x7f:
			if sb.Len()+utf8.RuneLen(r) > egress.MaxReasonBytes {
				return strings.Join(strings.Fields(sb.String()), " ")
			}
			sb.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}
