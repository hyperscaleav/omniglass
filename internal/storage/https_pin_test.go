package storage_test

import (
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// https is its own entry over the tls handshake (#902, decision 4 of #1113):
// its endpoint is named https beside an http one on the same component, its
// public key pin rides the worklist as a tls endpoint's does, an operator
// accepts a changed key with :retry and certificate, and its bare mode is
// checked at write. A plain http endpoint pins nothing.
func TestAnHTTPSEndpointPinsItsCertificateKey(t *testing.T) {
	f := newTLSFixture(t)
	plain, err := f.gw.CreateEndpoint(f.ctx, "", storage.EndpointSpec{Transport: "http", Component: &f.component, Node: &f.node,
		Params: []byte(`{"target":"10.0.0.9:80"}`)}, f.all)
	if err != nil {
		t.Fatalf("an http endpoint: %v", err)
	}
	ep, err := f.gw.CreateEndpoint(f.ctx, "", storage.EndpointSpec{Transport: "https", Component: &f.component, Node: &f.node,
		Params: []byte(`{"target":"10.0.0.9:443","tls-mode":"pin"}`)}, f.all)
	if err != nil {
		t.Fatalf("an https endpoint beside the http one: %v", err)
	}
	if ep.Name != "https" || plain.Name != "http" {
		t.Fatalf("names %q and %q, want https and http", ep.Name, plain.Name)
	}
	before := f.task(t, ep.ID)
	if err := insertProperties(f.ctx, f.gw, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: f.component,
		Key: "tls-certificate", Instance: ep.Name, Value: key("A"), Source: "https", TS: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	trusted := f.task(t, ep.ID)
	if trusted.CertificatePin != key("A") || trusted.Fingerprint != before.Fingerprint {
		t.Fatalf("worklist pin %q (fingerprint moved %v), want the trusted key and an unmoved fingerprint", trusted.CertificatePin, trusted.Fingerprint != before.Fingerprint)
	}
	if _, err := f.gw.RetryEndpoint(f.ctx, "", ep.ID, storage.RetryPin{Certificate: key("C")}, f.all, f.all); err != nil {
		t.Fatalf("retry with a certificate on https: %v", err)
	}
	accepted := f.task(t, ep.ID)
	if accepted.CertificatePin != key("C") || accepted.Fingerprint == trusted.Fingerprint {
		t.Fatalf("after the retry: pin %q, fingerprint moved %v", accepted.CertificatePin, accepted.Fingerprint != trusted.Fingerprint)
	}
	if _, err := f.gw.RetryEndpoint(f.ctx, "", plain.ID, storage.RetryPin{Certificate: key("C")}, f.all, f.all); !errors.Is(err, storage.ErrRetryHostKey) {
		t.Fatalf("a certificate on a plain http endpoint = %v, want ErrRetryHostKey", err)
	}
	if got := f.task(t, plain.ID).CertificatePin; got != "" {
		t.Fatalf("a plain http endpoint carries a pin %q", got)
	}
	if _, err := f.gw.CreateEndpoint(f.ctx, "", storage.EndpointSpec{Transport: "https", Component: &f.component, Node: &f.node,
		Params: []byte(`{"target":"10.0.0.9:8443","tls-mode":"trusting"}`)}, f.all); !errors.Is(err, storage.ErrEndpointParamsInvalid) {
		t.Fatalf("a bare https endpoint with a mode that is not one = %v, want ErrEndpointParamsInvalid", err)
	}
}
