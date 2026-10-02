package collection

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// HTTPProber is the layer-7 HTTP probe boundary (#812), faked in unit tests so
// collection logic is hermetic. The ladder distinction it exists to draw: a
// port that ACCEPTS a connection but whose API never answers an HTTP request
// is reached-but-not-responsive, a different fact from port-closed, and only a
// real request can observe it. An outcome is DATA; err is reserved for a
// target that could not be attempted at all (an unresolved host), which the
// caller treats as inconclusive.
type HTTPProber interface {
	Probe(ctx context.Context, target string, timeout time.Duration) (HTTPResult, error)
}

// HTTPResult is one HTTP probe observation. Dialed says the TCP connect
// succeeded (the L4 rung); Responded says a syntactically valid HTTP response
// was drawn (the L7 rung), whatever its status code: a 500 is a responsive
// API telling us something, silence is not. ResponseMS is set only when
// Responded. Reason classifies the failing rung when one failed.
type HTTPResult struct {
	Dialed     bool
	Responded  bool
	Status     int
	DialMS     float64
	ResponseMS float64
	Reason     Reachability
}

// NewHTTPProber returns the real HTTP probe: one GET / over plain HTTP to the
// target (host:port), redirects not followed (a redirect IS a response), TLS
// and paths arriving with the driver spec (#813). The dial is hooked so a
// refused or dropped connect classifies on the L4 rung while a connect that
// succeeds and then draws no response classifies on the L7 rung.
func NewHTTPProber() HTTPProber { return httpProber{} }

type httpProber struct{}

func (httpProber) Probe(ctx context.Context, target string, timeout time.Duration) (HTTPResult, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	var res HTTPResult
	dialer := &net.Dialer{Timeout: timeout}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialStart := time.Now()
			conn, err := dialer.DialContext(ctx, network, addr)
			if err == nil {
				res.Dialed = true
				res.DialMS = float64(time.Since(dialStart)) / float64(time.Millisecond)
			}
			return conn, err
		},
		DisableKeepAlives: true,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+target+"/", nil)
	if err != nil {
		return HTTPResult{}, fmt.Errorf("collection: http probe %s: %w", target, err)
	}
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
