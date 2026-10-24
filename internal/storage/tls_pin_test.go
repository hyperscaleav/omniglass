package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// The tls transport on the server side (#1035): the worklist delivers the
// pin a tls endpoint holds (the operator's declared tls-certificate, else
// the key the node trusted on first use), the declared pin alone moves the
// connection fingerprint, an operator accepts a changed key through :retry
// with certificate (audited), and the verification mode is a driver input
// that edits in place and moves the fingerprint. Skipped under -short.

type tlsFixture struct {
	ctx       context.Context
	gw        *storage.PG
	dsn       string
	all       scope.Set
	node      string
	component string
}

func newTLSFixture(t *testing.T) *tlsFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	f := &tlsFixture{ctx: ctx, gw: gw, dsn: dsn, all: scope.Set{All: true}, node: "edge-t", component: "panel-t"}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: f.component}, f.all, f.all, f.all, f.all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: f.node}, f.all, f.all); err != nil {
		t.Fatal(err)
	}
	return f
}

// task is the worklist's first task for the endpoint.
func (f *tlsFixture) task(t *testing.T, endpointID string) storage.WorklistTask {
	t.Helper()
	wl, err := f.gw.NodeWorklist(f.ctx, f.node)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range wl.Tasks {
		if task.EndpointID == endpointID {
			return task
		}
	}
	t.Fatalf("no worklist task for endpoint %s in %+v", endpointID, wl.Tasks)
	return storage.WorklistTask{}
}

func key(c string) string { return "SHA256:" + strings.Repeat(c, 43) }

func TestTLSPinRidesTheWorklistAndRetryAcceptsAChangedKey(t *testing.T) {
	f := newTLSFixture(t)
	ep, err := f.gw.CreateEndpoint(f.ctx, "", storage.EndpointSpec{Transport: "tls", Component: &f.component, Node: &f.node,
		Params: []byte(`{"target":"10.0.0.9:51326","tls_mode":"pin"}`)}, f.all)
	if err != nil {
		t.Fatalf("a tls endpoint: %v", err)
	}
	if ep.Name != "tls" {
		t.Fatalf("endpoint name %q, want tls (named by its transport)", ep.Name)
	}
	before := f.task(t, ep.ID)
	if before.CertificatePin != "" || before.HostKey != "" {
		t.Fatalf("a fresh endpoint carries a pin: %+v", before)
	}

	// The node trusts a key on first use: it rides back as the pin, and
	// (a learned fact) moves nothing.
	if err := f.gw.InsertPropertySamples(f.ctx, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: f.component,
		Key: "tls-certificate", Instance: ep.Name, Value: key("A"), Source: "tls", TS: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	trusted := f.task(t, ep.ID)
	if trusted.CertificatePin != key("A") || trusted.HostKey != "" {
		t.Fatalf("worklist pin = %q (host key %q), want the trusted key as the certificate pin", trusted.CertificatePin, trusted.HostKey)
	}
	if trusted.Fingerprint != before.Fingerprint {
		t.Fatal("a key trusted on first use moved the fingerprint")
	}

	// Refused at the door: a malformed key, a host key on a tls endpoint.
	if _, err := f.gw.RetryEndpoint(f.ctx, "", ep.ID, storage.RetryPin{Certificate: "SHA256:nope"}, f.all, f.all); !errors.Is(err, storage.ErrRetryPin) {
		t.Fatalf("a malformed certificate key = %v, want ErrRetryPin", err)
	}
	if _, err := f.gw.RetryEndpoint(f.ctx, "", ep.ID, storage.RetryPin{HostKey: key("B")}, f.all, f.all); !errors.Is(err, storage.ErrRetryPin) {
		t.Fatalf("a host key on a tls endpoint = %v, want ErrRetryPin", err)
	}

	// The operator accepts the offered key: it is declared, wins over the
	// trusted one, moves the fingerprint, and the retry is audited with it.
	if _, err := f.gw.RetryEndpoint(f.ctx, "", ep.ID, storage.RetryPin{Certificate: key("C")}, f.all, f.all); err != nil {
		t.Fatalf("retry with the offered key: %v", err)
	}
	accepted := f.task(t, ep.ID)
	if accepted.CertificatePin != key("C") {
		t.Fatalf("worklist pin = %q, want the declared key", accepted.CertificatePin)
	}
	if accepted.Fingerprint == trusted.Fingerprint {
		t.Fatal("the declared pin left the fingerprint")
	}
	// A later observation of the old key does not displace the declared one.
	if err := f.gw.InsertPropertySamples(f.ctx, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: f.component,
		Key: "tls-certificate", Instance: ep.Name, Value: key("A"), Source: "tls", TS: time.Now().Add(time.Second)}}); err != nil {
		t.Fatal(err)
	}
	if got := f.task(t, ep.ID).CertificatePin; got != key("C") {
		t.Fatalf("worklist pin = %q after a stale observation, want the declared key", got)
	}
	conn, err := pgx.Connect(f.ctx, f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(f.ctx)
	var after []byte
	if err := conn.QueryRow(f.ctx, `select new from audit_log where resource = 'endpoint' and verb = 'retry' and resource_id = $1 order by id desc limit 1`, ep.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	var audited map[string]any
	_ = json.Unmarshal(after, &audited)
	if audited["certificate"] != key("C") {
		t.Fatalf("the retry's audit row = %s, want the accepted certificate key", after)
	}
}

// Only a tls endpoint pins a certificate key.
func TestRetryRefusesACertificateOffATLSEndpoint(t *testing.T) {
	f := newTLSFixture(t)
	ep, err := f.gw.CreateEndpoint(f.ctx, "", storage.EndpointSpec{Transport: "ssh", Component: &f.component, Node: &f.node,
		Params: []byte(`{"target":"10.0.0.9:22"}`)}, f.all)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.RetryEndpoint(f.ctx, "", ep.ID, storage.RetryPin{Certificate: key("A")}, f.all, f.all); !errors.Is(err, storage.ErrRetryPin) {
		t.Fatalf("a certificate on an ssh endpoint = %v, want ErrRetryPin", err)
	}
	if _, err := f.gw.RetryEndpoint(f.ctx, "", ep.ID, storage.RetryPin{HostKey: key("A")}, f.all, f.all); err != nil {
		t.Fatalf("a host key on an ssh endpoint refused: %v", err)
	}
}

// The verification mode is a driver input (decision 3 of #1076): the
// seeded newtron-nvp-tls declares pin, which the attach bakes into the
// endpoint's params for the node to read; an operator edits it in place,
// which moves the fingerprint (so a gate lifts); an unknown mode is refused
// naming the input; a label edit moves nothing.
func TestTLSModeIsADriverInputThatEditsInPlace(t *testing.T) {
	f := newTLSFixture(t)
	drv := "newtron-nvp-tls"
	ep, err := f.gw.CreateEndpoint(f.ctx, "", storage.EndpointSpec{Driver: &drv, Component: &f.component, Node: &f.node,
		Inputs: map[string]string{"host": "10.0.0.9"}, SecretRead: f.all}, f.all)
	if err != nil {
		t.Fatalf("attach newtron-nvp-tls: %v", err)
	}
	var params map[string]string
	_ = json.Unmarshal(ep.Params, &params)
	if params["tls_mode"] != "pin" || params["target"] != "10.0.0.9:51326" {
		t.Fatalf("attached params %s, want the driver's default mode, pin, and its port", ep.Params)
	}
	if _, err := f.gw.CreateEndpoint(f.ctx, "", storage.EndpointSpec{Driver: &drv, Component: &f.component, Node: &f.node,
		Inputs: map[string]string{"host": "10.0.0.9", "tls_mode": "trust-me"}, SecretRead: f.all}, f.all); err == nil || !strings.Contains(err.Error(), "tls_mode") {
		t.Fatalf("an attach with an unknown mode = %v, want refused naming tls_mode", err)
	}

	before := f.task(t, ep.ID).Fingerprint
	label := "Panel over TLS"
	if _, err := f.gw.UpdateEndpoint(f.ctx, "", ep.ID, storage.EndpointPatch{Label: &label}, f.all, f.all); err != nil {
		t.Fatal(err)
	}
	if f.task(t, ep.ID).Fingerprint != before {
		t.Fatal("a label edit moved the fingerprint")
	}
	if _, err := f.gw.UpdateEndpoint(f.ctx, "", ep.ID, storage.EndpointPatch{Inputs: map[string]string{"tls_mode": "maybe"}, SecretRead: f.all}, f.all, f.all); !errors.Is(err, storage.ErrEndpointPatchInvalid) || !strings.Contains(err.Error(), "tls_mode") {
		t.Fatalf("an unknown mode edit = %v, want ErrEndpointPatchInvalid naming tls_mode", err)
	}
	edited, err := f.gw.UpdateEndpoint(f.ctx, "", ep.ID, storage.EndpointPatch{Inputs: map[string]string{"tls_mode": "skip"}, SecretRead: f.all}, f.all, f.all)
	if err != nil {
		t.Fatalf("edit the mode to skip: %v", err)
	}
	_ = json.Unmarshal(edited.Params, &params)
	if params["tls_mode"] != "skip" || params["target"] != "10.0.0.9:51326" {
		t.Fatalf("edited params %s, want skip beside the unchanged target", edited.Params)
	}
	if f.task(t, ep.ID).Fingerprint == before {
		t.Fatal("editing the mode left the fingerprint")
	}
	// Clearing the input falls back to the driver's default.
	cleared, err := f.gw.UpdateEndpoint(f.ctx, "", ep.ID, storage.EndpointPatch{Inputs: map[string]string{"tls_mode": ""}, SecretRead: f.all}, f.all, f.all)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(cleared.Params, &params)
	if params["tls_mode"] != "pin" {
		t.Fatalf("cleared params %s, want the default back", cleared.Params)
	}
}
