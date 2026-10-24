package collection

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// HTTPProber is the layer-7 HTTP probe boundary (#812), faked in unit tests so
// collection logic is hermetic. The ladder distinction it exists to draw: a
// port that ACCEPTS a connection but whose API never answers an HTTP request
// is reached-but-not-responsive, a different fact from port-closed, and only a
// real request can observe it. An outcome is DATA; err is reserved for a
// target that could not be attempted at all (an unresolved host), which the
// caller treats as inconclusive. Over https (#902) the handshake runs first
// under the endpoint's trust; the probe never carries a credential (decision
// 7 of #1113), so reachability stays observed beneath an auth gate.
type HTTPProber interface {
	Probe(ctx context.Context, t HTTPTask) (HTTPResult, error)
}

// HTTPResult is one HTTP probe observation. Dialed says the TCP connect
// succeeded (the L4 rung); Responded says a syntactically valid HTTP response
// was drawn (the L7 rung), whatever its status code: a 500 is a responsive
// API telling us something, silence is not. ResponseMS is set only when
// Responded. Reason classifies the failing rung when one failed. Over https,
// Handshaken says the component answered the handshake with a certificate
// (the transport rung), Accepted that the trust accepted it, Facts what it
// observed, and Refusal a certificate refusal the gate holds.
type HTTPResult struct {
	Dialed      bool
	Responded   bool
	Status      int
	DialMS      float64
	ResponseMS  float64
	Reason      Reachability
	Handshaken  bool
	Accepted    bool
	HandshakeMS float64
	Facts       TLSFacts
	Refusal     *TerminalError
}

// NewHTTPProber returns the real HTTP probe: one GET / to the target
// (host:port), over plain HTTP or, with a trust, over the TLS handshake,
// redirects not followed (a redirect IS a response), no credential. The dial
// is hooked so a refused or dropped connect classifies on the L4 rung while
// a connect that succeeds and then draws no response classifies on the L7
// rung.
func NewHTTPProber() HTTPProber { return httpProber{} }

type httpProber struct{}

func (httpProber) Probe(ctx context.Context, t HTTPTask) (HTTPResult, error) {
	timeout := t.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	var res HTTPResult
	dialer := &net.Dialer{Timeout: timeout}
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialStart := time.Now()
		conn, err := dialer.DialContext(ctx, network, addr)
		if err == nil {
			res.Dialed = true
			res.DialMS = float64(time.Since(dialStart)) / float64(time.Millisecond)
		}
		return conn, err
	}
	transport := &http.Transport{DialContext: dial, DisableKeepAlives: true}
	scheme := "http"
	if t.TLS != nil {
		scheme = "https"
		trust := *t.TLS
		transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := dial(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			tc, facts, err := HandshakeTLS(ctx, conn, trust, timeout)
			res.Facts, res.HandshakeMS = facts, facts.HandshakeMS
			if err != nil {
				if terr, ok := AsTerminal(err); ok && terr.Class == driver.ClassCertificate {
					// The protocol answered with a certificate; it is the
					// certificate that is refused.
					res.Handshaken, res.Refusal = true, terr
				}
				return nil, err
			}
			res.Handshaken, res.Accepted = true, true
			return tc, nil
		}
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scheme+"://"+t.Target+"/", nil)
	if err != nil {
		return HTTPResult{}, fmt.Errorf("collection: http probe %s: %w", t.Target, err)
	}
	req.Header.Set("User-Agent", "omniglass-node")
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		if !res.Dialed {
			// The L4 rung failed: classify like the tcp probe does. A failure
			// that classifies is data; one that does not (resolve/setup) is
			// inconclusive.
			if reason, ok := Classify(err); ok {
				res.Reason = reason
				return res, nil
			}
			return HTTPResult{}, err
		}
		if res.Refusal != nil {
			res.Reason = Responded
			return res, nil
		}
		// Dialed but no HTTP response: the reached-but-not-responsive fact
		// this probe exists to observe. A timeout, a hang, or line noise all
		// read the same on this rung.
		res.Reason = Timedout
		if reason, ok := Classify(err); ok {
			res.Reason = reason
		}
		return res, nil
	}
	defer func() { _ = resp.Body.Close() }()
	res.Responded = true
	res.Status = resp.StatusCode
	res.ResponseMS = float64(time.Since(start)) / float64(time.Millisecond)
	res.Reason = Responded
	return res, nil
}
