package storage_test

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestOutletOffHoldsWhatTheOutletFeeds pins #1653 on the shipped catalog: the
// seeded outlet-off, issued on a PDU attached through the shipped
// ferrite-pdu-api driver with outlet 2 in its params, is delivered as a
// disruptive command whose hold set is every endpoint of the codec fed from
// outlet 2, its out-of-band one included. The PDU's own endpoints (it switches
// the outlet and stays up) and the display fed from outlet 3 are not held.
// The rendered request carries the outlet as a number and the fixed state.
func TestOutletOffHoldsWhatTheOutletFeeds(t *testing.T) {
	t.Parallel()
	f := newDepFixture(t)
	ctx := context.Background()
	for _, c := range []string{"pdu-o", "codec-o", "display-o"} {
		f.component(t, c)
	}
	attachFerritePDUAPI(t, f, "pdu-o")
	f.endpoint(t, "pdu-o", "icmp", false)
	codecAPI := f.endpoint(t, "codec-o", "http", false)
	codecBMC := f.endpoint(t, "codec-o", "icmp", true)
	f.endpoint(t, "display-o", "icmp", false)
	f.depend(t, "codec-o", "pdu-o", storage.DependencyPower, "2")
	f.depend(t, "display-o", "pdu-o", storage.DependencyPower, "3")

	if _, err := f.gw.IssueCommand(ctx, f.actor, "component", "pdu-o", "outlet-off", "", nil, json.RawMessage(`{"outlet":2}`)); err != nil {
		t.Fatalf("issue outlet-off: %v", err)
	}
	d := f.delivery(t, "outlet-off")
	if !d.Disruptive || d.QuietSeconds <= 0 || d.HoldSeconds <= d.QuietSeconds {
		t.Errorf("delivery = %+v, want disruptive with a quiet and a longer hold", d)
	}
	if want := sorted(codecAPI, codecBMC); !slices.Equal(d.HoldEndpoints, want) {
		t.Fatalf("outlet-off hold set = %v, want every endpoint of the codec on outlet 2 %v", d.HoldEndpoints, want)
	}
	if d.Request == nil || d.Request.Method != "POST" || d.Request.Path != "/api/outlets/state" {
		t.Fatalf("request = %+v, want POST /api/outlets/state", d.Request)
	}
	var body map[string]any
	if err := json.Unmarshal(d.Request.Body, &body); err != nil || body["outlet"] != float64(2) || body["state"] != "off" {
		t.Fatalf("request body = %s (%v), want {\"outlet\": 2, \"state\": \"off\"}", d.Request.Body, err)
	}
}

// TestOutletOffHoldsHoweverTheOutletIsSpelled: outlet-off's schema types the
// outlet an integer, which 2.0 and 2e0 both are, and the PDU switches outlet 2
// for each. The hold set must follow the outlet the PDU switches, so every
// spelling of 2 holds the codec on outlet 2, not only the bare digits the
// dependency names it by.
func TestOutletOffHoldsHoweverTheOutletIsSpelled(t *testing.T) {
	t.Parallel()
	f := newDepFixture(t)
	ctx := context.Background()
	for _, c := range []string{"pdu-s", "codec-s"} {
		f.component(t, c)
	}
	attachFerritePDUAPI(t, f, "pdu-s")
	codecAPI := f.endpoint(t, "codec-s", "http", false)
	f.depend(t, "codec-s", "pdu-s", storage.DependencyPower, "2")

	for _, params := range []string{`{"outlet":2}`, `{"outlet":2.0}`, `{"outlet":2e0}`, `{"outlet":20e-1}`} {
		if _, err := f.gw.IssueCommand(ctx, f.actor, "component", "pdu-s", "outlet-off", "", nil, json.RawMessage(params)); err != nil {
			t.Fatalf("issue outlet-off %s: %v", params, err)
		}
		d := f.delivery(t, "outlet-off")
		if !slices.Equal(d.HoldEndpoints, []string{codecAPI}) {
			t.Errorf("outlet-off %s hold set = %v, want the codec on outlet 2 [%s]", params, d.HoldEndpoints, codecAPI)
		}
	}
}

// TestOutletOnHoldsNothing: switching an outlet on takes nothing down, so its
// delivery is not disruptive and holds no endpoint, even with a component on
// that outlet.
func TestOutletOnHoldsNothing(t *testing.T) {
	t.Parallel()
	f := newDepFixture(t)
	ctx := context.Background()
	for _, c := range []string{"pdu-n", "codec-n"} {
		f.component(t, c)
	}
	attachFerritePDUAPI(t, f, "pdu-n")
	f.endpoint(t, "codec-n", "http", false)
	f.depend(t, "codec-n", "pdu-n", storage.DependencyPower, "2")

	if _, err := f.gw.IssueCommand(ctx, f.actor, "component", "pdu-n", "outlet-on", "", nil, json.RawMessage(`{"outlet":2}`)); err != nil {
		t.Fatalf("issue outlet-on: %v", err)
	}
	d := f.delivery(t, "outlet-on")
	if d.Disruptive || len(d.HoldEndpoints) != 0 {
		t.Fatalf("outlet-on delivery = %+v, want not disruptive and holding nothing", d)
	}
}

// attachFerritePDUAPI attaches the shipped ferrite-pdu-api driver to a PDU on
// the fixture's node, its login a basic-auth secret. A secret is sealed, so
// this goes through a gateway holding a key; the fixture's own gateway reads
// the command wire, which never carries the credential.
func attachFerritePDUAPI(t *testing.T, f *depFixture, pdu string) {
	t.Helper()
	ctx := context.Background()
	all := scope.Set{All: true}
	gw, err := storage.NewPG(ctx, f.dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open keyed gateway: %v", err)
	}
	defer gw.Close()
	login := pdu + "-login"
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: login, SecretType: "basic-auth", OwnerKind: "platform",
		Fields: map[string]string{"username": "ferrite", "password": "ferrite"},
	}, all, true); err != nil {
		t.Fatalf("create secret: %v", err)
	}
	drv, node := "ferrite-pdu-api", f.node
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &drv, Component: &pdu, Node: &node,
		Inputs:     map[string]string{"host": "10.20.9.20", "login": login},
		SecretRead: all, CanAdmin: true,
	}, all); err != nil {
		t.Fatalf("attach ferrite-pdu-api: %v", err)
	}
}
