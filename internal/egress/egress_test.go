package egress_test

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/egress"
)

// The egress contract (#618) proved against real sockets: an httptest server
// on loopback is exactly the internal destination the contract refuses by
// default, so every test here dials a real address and the refusal is the
// dialer's, not a fake's.

func mustPolicy(t *testing.T, entries ...string) egress.Policy {
	t.Helper()
	p, err := egress.ParsePolicy(entries)
	if err != nil {
		t.Fatalf("ParsePolicy(%v): %v", entries, err)
	}
	return p
}

func TestParsePolicyReadsCIDRsAddressesAndHostsAndRefusesATypo(t *testing.T) {
	p := mustPolicy(t, "10.0.0.0/8", "192.168.1.20", "chat.internal", " ")
	if len(p.Prefixes) != 2 || len(p.Hosts) != 1 {
		t.Fatalf("policy = %+v", p)
	}
	for _, bad := range []string{"10.0.0.0/33", "http://chat", "chat internal", "a/b"} {
		if _, err := egress.ParsePolicy([]string{bad}); err == nil {
			t.Errorf("ParsePolicy(%q) = nil error, want a refusal naming it", bad)
		}
	}
}

func TestCheckAddrRefusesInternalAndMetadataUnlessAllowed(t *testing.T) {
	none := egress.Policy{}
	for _, a := range []string{"127.0.0.1", "10.1.2.3", "172.20.0.5", "192.168.0.1", "169.254.1.1", "100.64.0.1", "::1", "fd12::1", "fe80::1", "::ffff:127.0.0.1"} {
		if err := none.CheckAddr("x", netip.MustParseAddr(a)); !errors.Is(err, egress.ErrRefused) {
			t.Errorf("CheckAddr(%s) = %v, want refused", a, err)
		}
	}
	if err := none.CheckAddr("x", netip.MustParseAddr("93.184.216.34")); err != nil {
		t.Errorf("a public address = %v, want allowed", err)
	}
	allow := mustPolicy(t, "10.0.0.0/8", "chat.internal", "169.254.0.0/16")
	if err := allow.CheckAddr("x", netip.MustParseAddr("10.9.9.9")); err != nil {
		t.Errorf("an allowed range = %v", err)
	}
	if err := allow.CheckAddr("chat.internal", netip.MustParseAddr("192.168.4.4")); err != nil {
		t.Errorf("an allowed host = %v", err)
	}
	// Metadata stays refused even inside an allowed range.
	if err := allow.CheckAddr("x", netip.MustParseAddr("169.254.169.254")); !errors.Is(err, egress.ErrRefused) {
		t.Errorf("metadata inside an allowed range = %v, want refused", err)
	}
}

func TestCheckURLAdmitsHTTPSAndPlainHTTPOnlyToAnAllowedDestination(t *testing.T) {
	p := mustPolicy(t, "chat.internal", "10.0.0.0/8")
	for _, ok := range []string{"https://hooks.example.com/x", "http://chat.internal/hook", "http://10.1.1.1:8080/h"} {
		if _, err := p.CheckURL(ok); err != nil {
			t.Errorf("CheckURL(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"http://hooks.example.com/x", "ftp://chat.internal/", "https://user:pw@hooks.example.com/", "https:///nohost", "not a url\x7f"} {
		if _, err := p.CheckURL(bad); !errors.Is(err, egress.ErrRefused) {
			t.Errorf("CheckURL(%q) = %v, want refused", bad, err)
		}
	}
}

func post(c *egress.Client, url string) egress.Result {
	return c.Post(context.Background(), egress.Request{URL: url, Header: http.Header{"Content-Type": {"application/json"}}, Body: []byte(`{}`)})
}

func TestPostRefusesALoopbackReceiverByDefaultAndReachesItWhenAllowed(t *testing.T) {
	var hits int
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits++ }))
	defer srv.Close()
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())

	// Named by "localhost", resolved to loopback at dial time: refused.
	url := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	res := post(egress.New(egress.Policy{}, egress.Options{RootCAs: roots}), url)
	if res.OK || !strings.Contains(res.Reason, "internal address") || hits != 0 {
		t.Fatalf("default policy = %+v (hits %d), want refused before any request", res, hits)
	}
	res = post(egress.New(mustPolicy(t, "127.0.0.1"), egress.Options{RootCAs: roots}), srv.URL)
	if !res.OK || res.Status != http.StatusOK || hits != 1 {
		t.Fatalf("allowed = %+v (hits %d), want delivered", res, hits)
	}
}

func TestPostFailsOnAnUntrustedCertificateWithTheReason(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	res := post(egress.New(mustPolicy(t, "127.0.0.1"), egress.Options{}), srv.URL)
	if res.OK || !strings.HasPrefix(res.Reason, "TLS verification failed") {
		t.Fatalf("untrusted cert = %+v, want a TLS failure", res)
	}
}

func TestPostDoesNotFollowARedirect(t *testing.T) {
	var landed bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			landed = true
			return
		}
		http.Redirect(w, r, "/elsewhere", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	res := post(egress.New(mustPolicy(t, "127.0.0.1"), egress.Options{}), srv.URL+"/hook")
	if res.OK || res.Status != http.StatusTemporaryRedirect || landed || !strings.Contains(res.Reason, "not followed") {
		t.Fatalf("redirect = %+v (landed %v), want the 307 as the answer", res, landed)
	}
}

func TestPostRecordsAFailureStatusAndABoundedReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("down for\nmaintenance " + strings.Repeat("x", 2000)))
	}))
	defer srv.Close()
	res := post(egress.New(mustPolicy(t, "127.0.0.1"), egress.Options{}), srv.URL)
	if res.OK || res.Status != 503 || !strings.Contains(res.Reason, "503: down for maintenance") || len(res.Reason) > egress.MaxReasonBytes+64 {
		t.Fatalf("503 = %+v", res)
	}
}

func TestPostGivesUpOnASlowReceiver(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)
	start := time.Now()
	res := post(egress.New(mustPolicy(t, "127.0.0.1"), egress.Options{ResponseTimeout: 200 * time.Millisecond}), srv.URL)
	if res.OK || res.Reason != "the receiver did not answer in time" || time.Since(start) > 5*time.Second {
		t.Fatalf("slow receiver = %+v after %s", res, time.Since(start))
	}
}

func TestPostReasonNeverCarriesTheURL(t *testing.T) {
	// A chat incoming-webhook URL is itself a credential: a failure must not
	// copy it into the delivery row.
	res := post(egress.New(egress.Policy{}, egress.Options{}), "https://localhost:1/services/T000/B000/SECRETPATH")
	if res.OK || strings.Contains(res.Reason, "SECRETPATH") {
		t.Fatalf("reason = %q, want no URL in it", res.Reason)
	}
}

// The security review's cases (#1320): a zoned IPv6 address matches no prefix,
// so it has to be refused before any prefix is consulted; an address that
// embeds an IPv4 destination is judged as that destination; the metadata set
// covers the agents beside the instance metadata service.
func TestCheckAddrJudgesTheAddressAZoneOrATranslationHides(t *testing.T) {
	none := egress.Policy{}
	for _, a := range []string{"::1%lo", "fe80::1%eth0", "fc00::1%lo", "fd00:ec2::254%eth0"} {
		if err := none.CheckAddr("x", netip.MustParseAddr(a)); !errors.Is(err, egress.ErrRefused) {
			t.Errorf("CheckAddr(%s) = %v, want refused", a, err)
		}
	}
	for _, a := range []string{"64:ff9b::a9fe:a9fe", "64:ff9b::7f00:1", "64:ff9b:1::a00:1", "2002:a00:1::1", "2001:0:a00:1::1", "::a00:1"} {
		if err := none.CheckAddr("x", netip.MustParseAddr(a)); !errors.Is(err, egress.ErrRefused) {
			t.Errorf("CheckAddr(%s) = %v, want refused", a, err)
		}
	}
	allowAll := mustPolicy(t, "0.0.0.0/0", "::/0")
	for _, a := range []string{"169.254.170.23", "169.254.0.23", "168.63.129.16", "fd00:ec2::23", "64:ff9b::a9fe:a9fe"} {
		if err := allowAll.CheckAddr("x", netip.MustParseAddr(a)); !errors.Is(err, egress.ErrRefused) {
			t.Errorf("metadata %s under an allow-all list = %v, want refused", a, err)
		}
	}
	if _, err := none.CheckURL("https://[::1%25lo]:443/x"); !errors.Is(err, egress.ErrRefused) {
		t.Errorf("a zoned URL host = %v, want refused", err)
	}
}

func TestPostRefusesAZonedLoopbackOnARealSocket(t *testing.T) {
	ln, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("no IPv6 loopback here: %v", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Listener = ln
	var handshakes int
	srv.Config.ConnState = func(net.Conn, http.ConnState) { handshakes++ }
	srv.StartTLS()
	defer srv.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	res := post(egress.New(egress.Policy{}, egress.Options{}), "https://[::1%25lo]:"+strconv.Itoa(port)+"/hooks/SECRETPATH")
	if res.OK || !strings.Contains(res.Reason, "zoned") || handshakes != 0 {
		t.Fatalf("zoned loopback = %+v (connections %d), want refused before any connection", res, handshakes)
	}
}

func TestAReceiverEchoNeverCarriesTheURLOrTheToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		// Pad so the echo straddles the reason's cut.
		_, _ = w.Write([]byte(strings.Repeat("p", egress.MaxReasonBytes-40) + "Cannot POST " + r.URL.Path + " with " + r.Header.Get("Authorization")))
	}))
	defer srv.Close()
	res := egress.New(mustPolicy(t, "127.0.0.1"), egress.Options{}).Post(context.Background(), egress.Request{
		URL: srv.URL + "/services/T000/B000/XXXXSECRET", Header: http.Header{"Authorization": {"Bearer TOKEN-1320"}}, Body: []byte("{}"),
	})
	if res.OK || res.Status != 404 || strings.Contains(res.Reason, "XXXX") || strings.Contains(res.Reason, "TOKEN") || strings.Contains(res.Reason, "B000") {
		t.Fatalf("echo reason = %q, want no URL path and no token", res.Reason)
	}
	res = egress.New(mustPolicy(t, "127.0.0.1"), egress.Options{}).Post(context.Background(), egress.Request{
		URL: srv.URL + "/a/LONGSECRETPATH-1320", Body: []byte("{}"),
	})
	if strings.Contains(res.Reason, "LONGSECR") {
		t.Fatalf("straddling echo reason = %q, want the path scrubbed before the cut", res.Reason)
	}
}
