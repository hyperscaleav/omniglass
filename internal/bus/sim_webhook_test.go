package bus_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestSimlistCarriesAWebhookSubscriptionToTheSimulatorAlone proves the fence
// of ADR-1370. The simulator plays the external SaaS that posts to a webhook
// endpoint, so it holds what that SaaS would: the endpoint's path (its token)
// and the key it signs with. Those ride only the simulator's device list
// (the simlist, pulled only by a node running the simulator), only for a
// simulated component (one its node's endpoints reach on a loopback address).
// A real device's webhook is never handed out, and the node's own worklist,
// which every node pulls, carries neither the token nor the key.
func TestSimlistCarriesAWebhookSubscriptionToTheSimulatorAlone(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t),
		storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "node-a"}, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.SetEnrollmentToken(ctx, "", "node-a", hashHex("secret-a"), all); err != nil {
		t.Fatal(err)
	}
	const key = "ucc-signing-key-1370"
	node := "node-a"
	// ucc-sim is simulated (its probe reaches a loopback address); ucc-real
	// is a real service on the network, webhook and all.
	for name, target := range map[string]string{"ucc-sim": "127.21.0.7:443", "ucc-real": "10.20.30.40:443"} {
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name}, all, all, all, all); err != nil {
			t.Fatal(err)
		}
		comp := name
		if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{Name: "signing-" + name, SecretType: "hmac-key", OwnerKind: "component",
			OwnerName: &comp, Fields: map[string]string{"key": key}}, all, true); err != nil {
			t.Fatal(err)
		}
		drv := "ucc-webhook"
		if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Driver: &drv, Component: &comp,
			Inputs: map[string]string{"signing-key": "signing-" + name}, SecretRead: all, CanAdmin: true}, all); err != nil {
			t.Fatal(err)
		}
		if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "tcp", Component: &comp, Node: &node,
			Params: []byte(`{"target":"` + target + `"}`)}, all); err != nil {
			t.Fatal(err)
		}
	}
	token := func(comp string) string {
		f, err := gw.RevealSecret(ctx, "", comp+"/"+storage.WebhookTokenSecret, all, all, true)
		if err != nil {
			t.Fatal(err)
		}
		return f["token"]
	}
	simTok, realTok := token("ucc-sim"), token("ucc-real")

	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown()
	nc, err := nats.Connect(srv.ClientURL(), nats.UserInfo("node-a", "secret-a"),
		nats.CustomInboxPrefix(collection.InboxPrefix("node-a")))
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()

	msg, err := nc.Request(collection.SimlistSubject("node-a"), []byte(`{}`), 3*time.Second)
	if err != nil {
		t.Fatalf("simlist request: %v", err)
	}
	var reply collection.SimlistReply
	if err := json.Unmarshal(msg.Data, &reply); err != nil {
		t.Fatal(err)
	}
	got := map[string]*collection.SimWebhook{}
	for _, d := range reply.Devices {
		got[d.Name] = d.Webhook
	}
	want := collection.SimWebhook{Path: "/api/v1/webhooks/" + simTok, Scheme: "hmac", Secret: key}
	if w := got["ucc-sim"]; w == nil || *w != want {
		t.Fatalf("the simulated service's subscription = %+v, want %+v", w, want)
	}
	if w, listed := got["ucc-real"]; !listed || w != nil {
		t.Fatalf("the real service: listed %v, subscription %+v; want it listed (its address is not the simulator's to serve) with no subscription", listed, w)
	}
	if strings.Contains(string(msg.Data), realTok) {
		t.Fatal("the simlist carries the real service's webhook token")
	}

	// The worklist, which a node pulls whether or not it simulates, carries
	// neither secret.
	wl, err := nc.Request(collection.WorklistSubject("node-a"), nil, 3*time.Second)
	if err != nil {
		t.Fatalf("worklist request: %v", err)
	}
	for what, s := range map[string]string{"the simulated service's token": simTok, "the real service's token": realTok, "the signing key": key} {
		if strings.Contains(string(wl.Data), s) {
			t.Errorf("the worklist carries %s", what)
		}
	}
}
