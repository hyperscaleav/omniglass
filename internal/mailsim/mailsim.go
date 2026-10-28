// Package mailsim is a mail relay simulator (#1317): a small SMTP server that
// accepts what it is sent and keeps it, so a test, the lab and a demo can
// prove an email notification end to end without a real relay and without
// sending mail anywhere. It speaks enough SMTP for the platform's sender:
// EHLO/HELO, STARTTLS (when given a certificate), AUTH PLAIN (any
// credential, recorded by username), MAIL, RCPT, DATA, RSET, NOOP and QUIT.
// It can be told to fail, answering every MAIL with a temporary error, which
// is how a relay outage is simulated.
package mailsim

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"mime"
	"mime/quotedprintable"
	"net"
	netmail "net/mail"
	"net/textproto"
	"strings"
	"sync"
	"time"
)

// Received is one message the sim accepted.
type Received struct {
	From      string    `json:"from"`
	To        []string  `json:"to"`
	Subject   string    `json:"subject"`
	Text      string    `json:"text"`
	MessageID string    `json:"message_id"`
	TLS       bool      `json:"tls"`
	User      string    `json:"user,omitempty"`
	Raw       []byte    `json:"-"`
	At        time.Time `json:"at"`
}

// Options configure a sim: a certificate turns STARTTLS on; Out, when set,
// gets each accepted message as one JSON line; Failing, when set, is asked on
// every MAIL whether to refuse it.
type Options struct {
	TLS     *tls.Config
	Out     io.Writer
	Failing func() bool
}

// Server is a running sim.
type Server struct {
	ln   net.Listener
	opts Options
	mu       sync.Mutex
	msgs     []Received
	rejected []Received
	fail     bool
	reject   bool
	wg       sync.WaitGroup
}

// Start listens on addr ("127.0.0.1:0" for a test's free port) and serves
// until Close.
func Start(addr string, opts Options) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &Server{ln: ln, opts: opts}
	s.wg.Add(1)
	go s.serve()
	return s, nil
}

// Addr is the address it listens on.
func (s *Server) Addr() string { return s.ln.Addr().String() }

// Close stops it and waits for its sessions.
func (s *Server) Close() error {
	err := s.ln.Close()
	s.wg.Wait()
	return err
}

// SetFailing makes every MAIL answer a temporary error (true) or succeed.
func (s *Server) SetFailing(on bool) {
	s.mu.Lock()
	s.fail = on
	s.mu.Unlock()
}

// SetRejectData makes every message answer a temporary error after its whole
// body was read (true), as a relay failing at the last step does; the
// refused messages are kept apart (Rejected), so a test can compare what a
// retry sends with what the failed attempt sent.
func (s *Server) SetRejectData(on bool) {
	s.mu.Lock()
	s.reject = on
	s.mu.Unlock()
}

// Rejected are the messages refused after their body, oldest first.
func (s *Server) Rejected() []Received {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Received(nil), s.rejected...)
}

// Messages are the messages accepted so far, oldest first.
func (s *Server) Messages() []Received {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Received(nil), s.msgs...)
}

func (s *Server) failing() bool {
	s.mu.Lock()
	on := s.fail
	s.mu.Unlock()
	return on || (s.opts.Failing != nil && s.opts.Failing())
}

func (s *Server) serve() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(time.Minute))
			s.session(c)
		}()
	}
}

func (s *Server) session(c net.Conn) {
	tp := textproto.NewConn(c)
	reply := func(line string) { _ = tp.PrintfLine("%s", line) }
	reply("220 omniglass-mailsim ready")
	var cur Received
	secure := false
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		verb, arg, _ := strings.Cut(line, " ")
		switch strings.ToUpper(verb) {
		case "EHLO":
			exts := []string{"250-omniglass-mailsim", "250-AUTH PLAIN", "250-8BITMIME"}
			if s.opts.TLS != nil && !secure {
				exts = append(exts, "250-STARTTLS")
			}
			for _, e := range exts {
				reply(e)
			}
			reply("250 SMTPUTF8")
		case "HELO":
			reply("250 omniglass-mailsim")
		case "STARTTLS":
			if s.opts.TLS == nil || secure {
				reply("502 5.5.1 not offered")
				continue
			}
			reply("220 2.0.0 go ahead")
			tc := tls.Server(c, s.opts.TLS)
			if err := tc.Handshake(); err != nil {
				return
			}
			c, secure = tc, true
			tp = textproto.NewConn(tc)
			reply = func(line string) { _ = tp.PrintfLine("%s", line) }
			cur = Received{}
		case "AUTH":
			mech, resp, _ := strings.Cut(arg, " ")
			if !strings.EqualFold(mech, "PLAIN") {
				reply("504 5.5.4 only PLAIN")
				continue
			}
			raw, err := base64.StdEncoding.DecodeString(resp)
			if err != nil {
				reply("501 5.5.2 bad credential")
				continue
			}
			parts := bytes.Split(raw, []byte{0})
			if len(parts) == 3 {
				cur.User = string(parts[1])
			}
			reply("235 2.7.0 accepted")
		case "MAIL":
			if s.failing() {
				reply("451 4.3.0 the mail sim is down")
				continue
			}
			cur.From, cur.To = addrArg(arg), nil
			reply("250 2.1.0 ok")
		case "RCPT":
			cur.To = append(cur.To, addrArg(arg))
			reply("250 2.1.5 ok")
		case "DATA":
			if cur.From == "" || len(cur.To) == 0 {
				reply("503 5.5.1 MAIL and RCPT first")
				continue
			}
			reply("354 end with .")
			data, err := tp.ReadDotBytes()
			if err != nil {
				return
			}
			cur.Raw, cur.TLS, cur.At = data, secure, time.Now().UTC()
			s.mu.Lock()
			reject := s.reject
			if reject {
				s.rejected = append(s.rejected, cur)
			}
			s.mu.Unlock()
			if reject {
				cur = Received{User: cur.User}
				reply("451 4.3.0 the mail sim refused the message")
				continue
			}
			s.keep(cur)
			cur = Received{User: cur.User}
			reply("250 2.0.0 queued")
		case "RSET":
			cur = Received{User: cur.User}
			reply("250 2.0.0 ok")
		case "NOOP":
			reply("250 2.0.0 ok")
		case "QUIT":
			reply("221 2.0.0 bye")
			return
		default:
			reply("502 5.5.2 not implemented")
		}
	}
}

func addrArg(arg string) string {
	_, v, _ := strings.Cut(arg, ":")
	v = strings.TrimSpace(v)
	if i := strings.IndexByte(v, ' '); i >= 0 {
		v = v[:i]
	}
	return strings.Trim(v, "<>")
}

// keep parses an accepted message for its subject and text and records it.
func (s *Server) keep(r Received) {
	if m, err := netmail.ReadMessage(bytes.NewReader(r.Raw)); err == nil {
		dec := new(mime.WordDecoder)
		if subj, err := dec.DecodeHeader(m.Header.Get("Subject")); err == nil {
			r.Subject = subj
		}
		r.MessageID = m.Header.Get("Message-Id")
		var body []byte
		if strings.EqualFold(m.Header.Get("Content-Transfer-Encoding"), "quoted-printable") {
			body, _ = io.ReadAll(quotedprintable.NewReader(m.Body))
		} else {
			body, _ = io.ReadAll(m.Body)
		}
		r.Text = strings.TrimRight(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")
	}
	s.mu.Lock()
	s.msgs = append(s.msgs, r)
	s.mu.Unlock()
	if s.opts.Out != nil {
		line, _ := json.Marshal(r)
		s.mu.Lock()
		_, _ = s.opts.Out.Write(append(line, '\n'))
		s.mu.Unlock()
	}
}

// SelfSigned is a throwaway certificate for host (an address or a name) and
// the pool that trusts it, for a sim serving STARTTLS to a test.
func SelfSigned(host string) (*tls.Config, *x509.CertPool, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: host},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}, pool, nil
}
