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
// and the key it signs with. Any node credential may pull its simlist, so the
// fence is server-held facts the node cannot assert: the database carries the
// lab mark (only the lab's trusted seed sets it), and the component sits in
// the simulated block (127.60.0.0/16, which only the seed allocates). A
// database with no lab mark hands out nothing; a real device's webhook, or
// one on another loopback address, never; and the node's own worklist
// carries neither the token nor the key.
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
	// ucc-sim is simulated (its probe reaches the simulated block); ucc-real
	// is a real service on the network, webhook and all; ucc-local is a
	// service on the node's own host, a loopback address outside the block.
	for name, target := range map[string]string{"ucc-sim": "127.60.0.7:443", "ucc-real": "10.20.30.40:443", "ucc-local": "127.0.0.1:8443"} {
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

	pull := func() ([]byte, map[string]*collection.SimWebhook) {
		t.Helper()
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
		return msg.Data, got
	}

	// No lab mark: a node's simlist hands out no subscription at all.
	if data, got := pull(); got["ucc-sim"] != nil || strings.Contains(string(data), simTok) || strings.Contains(string(data), key) {
		t.Fatalf("with no lab mark the simlist carries a subscription: %s", data)
	}

	if err := gw.MarkLab(ctx); err != nil {
		t.Fatal(err)
	}
	data, got := pull()
	want := collection.SimWebhook{Path: "/api/v1/webhooks/" + simTok, Scheme: "hmac", Secret: key}
	if w := got["ucc-sim"]; w == nil || *w != want {
		t.Fatalf("the simulated service's subscription = %+v, want %+v", w, want)
	}
	for _, other := range []string{"ucc-real", "ucc-local"} {
		if w, listed := got[other]; !listed || w != nil {
			t.Fatalf("%s: listed %v, subscription %+v; want it listed with no subscription", other, listed, w)
		}
	}
	if strings.Contains(string(data), realTok) || strings.Contains(string(data), token("ucc-local")) {
		t.Fatal("the simlist carries a webhook token of a component outside the simulated block")
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
