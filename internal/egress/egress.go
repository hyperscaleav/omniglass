// Package egress is the server's one outbound HTTP client (#618): every send
// Omniglass makes to a destination an operator named (a notification target
// today; a webhook action, an ITSM sink or a mail API later) goes through it,
// so the safety contract is written once.
//
// The contract, each half enforced here and nowhere else:
//
//   - Destinations. A public address is reachable; a loopback, private,
//     link-local, unique-local, carrier-grade NAT, multicast or unspecified one
//     is refused unless the install's allow list (the notification.egress_allow
//     setting, CIDRs and host names) names it. A cloud metadata address is
//     refused even when allowed: there is no AV receiver there, only the
//     install's own credentials. The check runs on the address actually dialed,
//     after resolution, at connect time, so a name that resolves to a public
//     address when validated and a private one when used (DNS rebinding) is
//     still refused, and every address a name resolves to is checked.
//   - Scheme. https always; plain http only to a destination the allow list
//     names, the internal chat server or lab receiver that has no certificate.
//   - TLS is verified (the system roots, TLS 1.2 at least); a failed handshake
//     is a failed send with its reason, never a downgrade.
//   - Redirects are not followed: a 3xx is the send's answer, a failure.
//   - Every phase is bounded: dial, handshake, the response's headers, the
//     whole exchange, and the bytes of a response body read for its reason.
//   - No proxy from the environment: a proxy would dial on the client's behalf
//     and every address check above would judge the proxy instead.
package egress

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// The bounds on one send.
const (
	DialTimeout     = 5 * time.Second
	TLSTimeout      = 5 * time.Second
	ResponseTimeout = 10 * time.Second
	TotalTimeout    = 15 * time.Second
	// MaxReasonBytes is how much of a response body is read for the reason a
	// failed send records; the rest is discarded unread.
	MaxReasonBytes = 512
)

// ErrRefused is a destination the contract refuses: an address outside the
// allowed set, a scheme it does not admit, a malformed URL.
var ErrRefused = errors.New("egress: destination refused")

// Policy is the install's allow list: the private ranges and host names an
// admin has opened. The zero Policy opens nothing.
type Policy struct {
	Prefixes []netip.Prefix
	Hosts    []string
}

// ParsePolicy reads the allow list's entries: each a CIDR, a bare address, or
// a host name (matched exactly, case-insensitively). An entry that is none of
// those is an error naming it, so a typo is visible rather than silently
// allowing nothing.
func ParsePolicy(entries []string) (Policy, error) {
	var p Policy
	for _, raw := range entries {
		e := strings.TrimSpace(raw)
		if e == "" {
			continue
		}
		if pfx, err := netip.ParsePrefix(e); err == nil {
			p.Prefixes = append(p.Prefixes, pfx.Masked())
			continue
		}
		if a, err := netip.ParseAddr(e); err == nil {
			p.Prefixes = append(p.Prefixes, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
			continue
		}
		if !validHost(e) {
			return Policy{}, fmt.Errorf("egress: allow entry %q is neither a CIDR, an address nor a host name", e)
		}
		p.Hosts = append(p.Hosts, strings.ToLower(e))
	}
	return p, nil
}

func validHost(h string) bool {
	if len(h) > 253 || strings.ContainsAny(h, "/:@ ") {
		return false
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}

func (p Policy) hostAllowed(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, h := range p.Hosts {
		if h == host {
			return true
		}
	}
	return false
}

func (p Policy) addrAllowed(a netip.Addr) bool {
	for _, pfx := range p.Prefixes {
		if pfx.Contains(a) {
			return true
		}
	}
	return false
}

// metadata are the cloud instance metadata addresses: refused whatever the
// allow list says.
var metadata = []netip.Addr{
	netip.MustParseAddr("169.254.169.254"), // AWS, GCP, Azure, OpenStack, DigitalOcean
	netip.MustParseAddr("169.254.170.2"),   // AWS ECS task metadata
	netip.MustParseAddr("100.100.100.200"), // Alibaba Cloud
	netip.MustParseAddr("fd00:ec2::254"),   // AWS IPv6
}

// internalRanges are refused unless allowed: everything that is not the
// public internet.
var internalRanges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

// IsMetadata reports whether a is a cloud metadata address.
func IsMetadata(a netip.Addr) bool {
	a = a.Unmap()
	for _, m := range metadata {
		if a == m {
			return true
		}
	}
	return false
}

// IsInternal reports whether a is outside the public internet.
func IsInternal(a netip.Addr) bool {
	a = a.Unmap()
	for _, pfx := range internalRanges {
		if pfx.Contains(a) {
			return true
		}
	}
	return false
}

// CheckAddr judges one dialed address for a destination host: metadata is
// always refused; an internal address only when neither the address nor the
// host is on the allow list.
func (p Policy) CheckAddr(host string, a netip.Addr) error {
	a = a.Unmap()
	if IsMetadata(a) {
		return fmt.Errorf("%w: %s is a cloud metadata address, which is never reachable", ErrRefused, a)
	}
	if IsInternal(a) && !p.addrAllowed(a) && !p.hostAllowed(host) {
		return fmt.Errorf("%w: %s resolves to %s, an internal address the egress allow list does not name", ErrRefused, host, a)
	}
	return nil
}

// CheckURL judges a destination URL before any connection: it parses, names
// a host, carries no credentials, and is https, or http to a host or address
// the allow list names.
func (p Policy) CheckURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: the URL does not parse", ErrRefused)
	}
	if u.Host == "" || u.Hostname() == "" {
		return nil, fmt.Errorf("%w: the URL names no host", ErrRefused)
	}
	if u.User != nil {
		return nil, fmt.Errorf("%w: the URL carries credentials; bind a secret instead", ErrRefused)
	}
	switch u.Scheme {
	case "https":
		return u, nil
	case "http":
		host := u.Hostname()
		if p.hostAllowed(host) {
			return u, nil
		}
		if a, err := netip.ParseAddr(host); err == nil && p.addrAllowed(a.Unmap()) && !IsMetadata(a) {
			return u, nil
		}
		return nil, fmt.Errorf("%w: plain http is only for a destination the egress allow list names; use https", ErrRefused)
	}
	return nil, fmt.Errorf("%w: the scheme must be https", ErrRefused)
}

// Client is the outbound client under one Policy.
type Client struct {
	policy Policy
	http   *http.Client
}

// Options tune a Client for a test: RootCAs replaces the system roots so a
// test server's certificate verifies; Resolver replaces the system resolver.
type Options struct {
	RootCAs  *x509.CertPool
	Resolver *net.Resolver
	// ResponseTimeout replaces ResponseTimeout (and so bounds the whole
	// exchange too) when set: a test's slow receiver need not take ten
	// seconds to prove the bound.
	ResponseTimeout time.Duration
}

// New returns a client under policy.
func New(policy Policy, opts Options) *Client {
	dialer := &net.Dialer{Timeout: DialTimeout, Resolver: opts.Resolver}
	respTimeout, total := ResponseTimeout, TotalTimeout
	if opts.ResponseTimeout > 0 {
		respTimeout, total = opts.ResponseTimeout, opts.ResponseTimeout
	}
	// The address check runs in Control, on the socket about to connect: the
	// resolved address, after every name lookup, so what is judged is what is
	// dialed. The host is carried in by the dial below.
	transport := &http.Transport{
		Proxy:                  nil,
		ForceAttemptHTTP2:      true,
		TLSHandshakeTimeout:    TLSTimeout,
		ResponseHeaderTimeout:  respTimeout,
		MaxResponseHeaderBytes: 16 << 10,
		DisableKeepAlives:      true,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: opts.RootCAs},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			d := *dialer
			d.Control = func(_, address string, _ syscall.RawConn) error {
				ap, err := netip.ParseAddrPort(address)
				if err != nil {
					return fmt.Errorf("%w: unreadable address %q", ErrRefused, address)
				}
				return policy.CheckAddr(host, ap.Addr())
			}
			return d.DialContext(ctx, network, addr)
		},
	}
	return &Client{
		policy: policy,
		http: &http.Client{
			Transport: transport,
			Timeout:   total,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Request is one outbound POST: its URL, headers and body, sent as is.
type Request struct {
	URL    string
	Header http.Header
	Body   []byte
}

// Result is what a send got back: the status (0 when no response came) and,
// for a failure, a reason fit to record (no header values, no request body).
type Result struct {
	Status int
	OK     bool
	Reason string
}

// Post sends one request under the contract. A transport failure (refused
// destination, TLS, timeout, connection) and a non-2xx answer are both a
// Result with OK false and a Reason; the error return is reserved for a
// request that could not be built at all.
func (c *Client) Post(ctx context.Context, r Request) Result {
	u, err := c.policy.CheckURL(r.URL)
	if err != nil {
		return Result{Reason: err.Error()}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(string(r.Body)))
	if err != nil {
		return Result{Reason: "egress: the request could not be built"}
	}
	for k, vs := range r.Header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Result{Reason: reason(err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, MaxReasonBytes))
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	res := Result{Status: resp.StatusCode}
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		res.OK = true
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		res.Reason = fmt.Sprintf("the receiver answered %d, a redirect, which is not followed", resp.StatusCode)
	default:
		res.Reason = fmt.Sprintf("the receiver answered %d", resp.StatusCode)
		if s := strings.TrimSpace(printable(body)); s != "" {
			res.Reason += ": " + s
		}
	}
	return res
}

// reason words a transport error without the URL (which may itself be a
// credential: a chat incoming-webhook URL is one).
func reason(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	var cert *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	var host x509.HostnameError
	switch {
	case errors.Is(err, ErrRefused):
		return err.Error()
	case errors.As(err, &cert), errors.As(err, &unknown), errors.As(err, &host):
		return "TLS verification failed: " + err.Error()
	case errors.Is(err, context.DeadlineExceeded), isTimeout(err):
		return "the receiver did not answer in time"
	}
	var op *net.OpError
	if errors.As(err, &op) {
		if errors.Is(op.Err, ErrRefused) {
			return op.Err.Error()
		}
		return "the connection failed: " + op.Err.Error()
	}
	return "the send failed: " + err.Error()
}

func isTimeout(err error) bool {
	var t interface{ Timeout() bool }
	return errors.As(err, &t) && t.Timeout()
}

// printable keeps a response excerpt to one line of printable text.
func printable(b []byte) string {
	var sb strings.Builder
	for _, r := range string(b) {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			sb.WriteByte(' ')
		case r >= 0x20 && r != 0x7f:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
