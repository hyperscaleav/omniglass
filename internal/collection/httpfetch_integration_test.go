package collection_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The real http fetcher and the https probe against real listeners (the
// capability carve-out, #902): the request leaves with its shape and its
// credential, the answer comes back capped, and the TLS primitive judges
// the certificate under each mode.

func TestHTTPFetcherSendsTheRequestAndTheCredential(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	var seen *http.Request
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, body = r.Clone(context.Background()), mustRead(r.Body)
		switch r.URL.Path {
		case "/big":
			_, _ = w.Write([]byte(strings.Repeat("x", 64)))
		case "/move":
			http.Redirect(w, r, "/login", http.StatusFound)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer srv.Close()
	f := collection.NewHTTPFetcher()
	ctx := context.Background()
	target := srv.Listener.Addr().String()

	ans, err := f.Exchange(ctx, collection.HTTPExchange{Target: target, Timeout: 2 * time.Second,
		Request:    driver.Request{Method: "PUT", Path: "/api/v", Query: map[string]string{"a": "b"}, Body: []byte(`{"v":1}`)},
		Credential: &driver.HTTPCredential{Scheme: driver.SchemeAPIKey, Key: "k1", In: driver.APIKeyInHeader, Name: "X-Census-Key"}})
	if err != nil || ans.Status != 200 || string(ans.Body) != `{"ok":true}` || !ans.Credentialed {
		t.Fatalf("answer %+v, %v", ans, err)
	}
	if seen.Method != "PUT" || seen.URL.RawQuery != "a=b" || seen.Header.Get("X-Census-Key") != "k1" || string(body) != `{"v":1}` || seen.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("the component saw %s %s %v %q", seen.Method, seen.URL, seen.Header, body)
	}

	ans, err = f.Exchange(ctx, collection.HTTPExchange{Target: target, Request: driver.Request{Path: "/move"}})
	if err != nil || ans.Status != http.StatusFound || ans.Location != "/login" {
		t.Fatalf("a redirect was followed: %+v, %v", ans, err)
	}
	if _, ok := collection.AsRequestFault(collection.JudgeHTTPAnswer(ans, collection.HTTPExchange{})); !ok {
		t.Fatal("a redirect did not fault the request")
	}

	_, err = f.Exchange(ctx, collection.HTTPExchange{Target: target, Request: driver.Request{Path: "/big"}, BodyCap: 16})
	if fault, ok := collection.AsRequestFault(err); !ok || !strings.Contains(fault.Error(), "cap of 16") {
		t.Fatalf("an oversized body = %v, want a request fault naming the cap", err)
	}
}

// A request that draws no answer names its URL with a query key masked: the
// client's own error would carry it whole.
func TestHTTPFetcherNeverNamesAQueryKey(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close() // hang up without answering
		}
	}()
	defer func() { _ = ln.Close() }()
	_, err = collection.NewHTTPFetcher().Exchange(context.Background(), collection.HTTPExchange{Target: addr, Timeout: time.Second,
		Request:    driver.Request{Path: "/x"},
		Credential: &driver.HTTPCredential{Scheme: driver.SchemeAPIKey, Key: "very-secret-key", In: driver.APIKeyInQuery, Name: "apikey"}})
	if err == nil {
		t.Fatal("a hang-up drew an answer")
	}
	if strings.Contains(err.Error(), "very-secret-key") || !strings.Contains(err.Error(), "apikey=%5Bredacted%5D") {
		t.Fatalf("error %q, want the url named with its key masked", err)
	}
}

func TestHTTPSFetchAndProbeUnderEachMode(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"power":"on"}`))
	}))
	defer srv.Close()
	target := srv.Listener.Addr().String()
	f := collection.NewHTTPFetcher()
	ctx := context.Background()

	// verify: the test server's certificate chains to no system root.
	_, err := f.Exchange(ctx, collection.HTTPExchange{Target: target, TLS: &collection.TLSTrust{Mode: driver.TLSModeVerify, ServerName: "127.0.0.1"}, Request: driver.Request{Path: "/"}})
	if terr, ok := collection.AsTerminal(err); !ok || terr.Class != driver.ClassCertificate {
		t.Fatalf("verify against an unknown authority = %v, want a certificate refusal", err)
	}
	// pin: the first key is trusted, then held.
	pin := collection.NewHostKeyPin("")
	ans, err := f.Exchange(ctx, collection.HTTPExchange{Target: target, TLS: &collection.TLSTrust{Mode: driver.TLSModePin, Pin: pin}, Request: driver.Request{Path: "/"}})
	if err != nil || ans.Status != 200 || pin.Get() == "" {
		t.Fatalf("pin on first use = %+v, %v, pin %q", ans, err, pin.Get())
	}
	stale := collection.NewHostKeyPin("SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	_, err = f.Exchange(ctx, collection.HTTPExchange{Target: target, TLS: &collection.TLSTrust{Mode: driver.TLSModePin, Pin: stale}, Request: driver.Request{Path: "/"}})
	if terr, ok := collection.AsTerminal(err); !ok || terr.Class != driver.ClassCertificate || terr.Offered != pin.Get() {
		t.Fatalf("a changed key = %v, want a certificate refusal offering %s", err, pin.Get())
	}
	// skip: anything answering is trusted, by the operator's choice.
	if ans, err := f.Exchange(ctx, collection.HTTPExchange{Target: target, TLS: &collection.TLSTrust{Mode: driver.TLSModeSkip}, Request: driver.Request{Path: "/"}}); err != nil || ans.Status != 200 {
		t.Fatalf("skip = %+v, %v", ans, err)
	}

	// The probe: the handshake is the rung, then a response is drawn.
	runner := &collection.Runner{HTTP: collection.NewHTTPProber()}
	dps, res, err := runner.CollectHTTPResult(ctx, collection.HTTPTask{Target: target, Timeout: 2 * time.Second, TLS: &collection.TLSTrust{Mode: driver.TLSModePin, Pin: collection.NewHostKeyPin(pin.Get())}})
	if err != nil || !res.Handshaken || !res.Accepted || !res.Responded {
		t.Fatalf("the https probe = %+v, %v", res, err)
	}
	names := map[string]bool{}
	for _, d := range dps {
		names[d.Name] = true
	}
	for _, want := range []string{"tcp-open", "tls-handshake-time", "http-response-time", "endpoint-responsive", "tls-certificate-expiry", "tls-certificate-days-left"} {
		if !names[want] {
			t.Errorf("the https probe landed %v, missing %s", names, want)
		}
	}
	// A refused certificate: the rung answered, no certificate fact lands.
	dps, res, err = runner.CollectHTTPResult(ctx, collection.HTTPTask{Target: target, Timeout: 2 * time.Second, TLS: &collection.TLSTrust{Mode: driver.TLSModePin, Pin: stale}})
	if err != nil || res.Refusal == nil || res.Accepted || res.Responded {
		t.Fatalf("the probe meeting a changed key = %+v, %v", res, err)
	}
	for _, d := range dps {
		if d.Name == "tls-certificate-expiry" || d.Name == "http-response-time" {
			t.Fatalf("a refused certificate landed %s", d.Name)
		}
	}
}

// A plain http probe never sends a credential (decision 7 of #1113): a
// component that answers 401 to everything is responsive.
func TestHTTPProbeIsCredentialFree(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	res, err := collection.NewHTTPProber().Probe(context.Background(), collection.HTTPTask{Target: srv.Listener.Addr().String(), Timeout: 2 * time.Second})
	if err != nil || !res.Responded || res.Status != 401 || auth != "" {
		t.Fatalf("probe = %+v, %v, authorization %q", res, err, auth)
	}
}

func mustRead(r io.Reader) []byte {
	b, _ := io.ReadAll(r)
	return b
}

