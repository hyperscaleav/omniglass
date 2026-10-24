package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/jackc/pgx/v5"
)

// The udp transport on the server side (#1037): the listen port is a driver
// input the attach bakes into the endpoint's params, so it is a fingerprint
// input and an edit moves the fingerprint; the worklist carries the request
// rung; a command delivery carries its binding's retries (none unless the
// binding declares them). Skipped under -short.

func TestListenPortIsADriverInputAndMovesTheFingerprint(t *testing.T) {
	f := newTLSFixture(t)
	drv := "newtron-nvp-udp"
	ep, err := f.gw.CreateEndpoint(f.ctx, "", storage.EndpointSpec{Driver: &drv, Component: &f.component, Node: &f.node,
		Inputs: map[string]string{"host": "10.0.0.9"}, SecretRead: f.all}, f.all)
	if err != nil {
		t.Fatalf("attach newtron-nvp-udp: %v", err)
	}
	if ep.Transport != "udp" || ep.Name != "udp" {
		t.Fatalf("endpoint %s/%s, want a udp endpoint named by its transport", ep.Transport, ep.Name)
	}
	var params map[string]string
	_ = json.Unmarshal(ep.Params, &params)
	if params["target"] != "10.0.0.9:51327" {
		t.Fatalf("attached params %s, want the driver's port", ep.Params)
	}
	if _, set := params[driver.ListenPortInput]; set {
		t.Fatalf("attached params %s carry a listen port the operator never gave", ep.Params)
	}
	task := f.task(t, ep.ID)
	if task.Transport != "udp" || task.Ladder == nil || task.Ladder.Transport != driver.RungRequest || task.Ladder.Request == nil || task.Ladder.Request.Line != "GET INPUT" {
		t.Fatalf("worklist task transport %s ladder %+v, want the request rung", task.Transport, task.Ladder)
	}

	if _, err := f.gw.UpdateEndpoint(f.ctx, "", ep.ID, storage.EndpointPatch{Inputs: map[string]string{driver.ListenPortInput: "70000"}, SecretRead: f.all}, f.all, f.all); !errors.Is(err, storage.ErrEndpointPatchInvalid) || !strings.Contains(err.Error(), driver.ListenPortInput) {
		t.Fatalf("a listen port off the range = %v, want ErrEndpointPatchInvalid naming %s", err, driver.ListenPortInput)
	}
	before := task.Fingerprint
	edited, err := f.gw.UpdateEndpoint(f.ctx, "", ep.ID, storage.EndpointPatch{Inputs: map[string]string{driver.ListenPortInput: "51400"}, SecretRead: f.all}, f.all, f.all)
	if err != nil {
		t.Fatalf("set the listen port: %v", err)
	}
	_ = json.Unmarshal(edited.Params, &params)
	if params[driver.ListenPortInput] != "51400" || params["target"] != "10.0.0.9:51327" {
		t.Fatalf("edited params %s, want the listen port beside the target", edited.Params)
	}
	moved := f.task(t, ep.ID).Fingerprint
	if moved == before {
		t.Fatal("setting the listen port left the fingerprint")
	}
	if _, err := f.gw.UpdateEndpoint(f.ctx, "", ep.ID, storage.EndpointPatch{Inputs: map[string]string{driver.ListenPortInput: "51401"}, SecretRead: f.all}, f.all, f.all); err != nil {
		t.Fatal(err)
	}
	if f.task(t, ep.ID).Fingerprint == moved {
		t.Fatal("editing the listen port left the fingerprint")
	}
}

// A bare udp endpoint's params carry the listen port as given, so one the
// node cannot bind is refused where it is written.
func TestABareUDPEndpointsListenPortIsCheckedAtWrite(t *testing.T) {
	f := newTLSFixture(t)
	if _, err := f.gw.CreateEndpoint(f.ctx, "", storage.EndpointSpec{Transport: "udp", Component: &f.component, Node: &f.node,
		Params: []byte(`{"target":"10.0.0.9:51327","listen-port":"any"}`)}, f.all); !errors.Is(err, storage.ErrEndpointParamsInvalid) || !strings.Contains(err.Error(), driver.ListenPortInput) {
		t.Fatalf("a bare udp endpoint with listen port any = %v, want ErrEndpointParamsInvalid naming %s", err, driver.ListenPortInput)
	}
	for i, good := range []string{`"51400"`, `51400`} {
		comp := []string{"panel-u1", "panel-u2"}[i]
		if _, err := f.gw.CreateComponent(f.ctx, "", storage.ComponentSpec{Name: comp}, f.all, f.all, f.all, f.all); err != nil {
			t.Fatal(err)
		}
		if _, err := f.gw.CreateEndpoint(f.ctx, "", storage.EndpointSpec{Transport: "udp", Component: &comp, Node: &f.node,
			Params: []byte(`{"target":"10.0.0.9:51327","listen-port":` + good + `}`)}, f.all); err != nil {
			t.Fatalf("a bare udp endpoint with listen port %s: %v", good, err)
		}
	}
}

// udpNoRetrySpec is a udp driver whose command declares no retries.
const udpNoRetrySpec = `{"version":1,"transport":"udp",
 "inputs":[{"name":"host","kind":"string","required":true},{"name":"port","kind":"number","default":"9100"}],
 "ladder":{"transport":{"request":{"line":"PING","reply":{"prefix":"PONG"}}}},
 "commands":[{"command_type":"set-input","request":{"line":"SET INPUT ${value}","reply":{"prefix":"INPUT "}}}]}`

func TestAUDPCommandDeliveryCarriesItsRetries(t *testing.T) {
	f := newTLSFixture(t)
	if _, err := f.gw.CreateDriver(f.ctx, "", storage.Driver{Name: "udp-no-retry", Label: "UDP no retry", Version: "1.0.0", Spec: []byte(udpNoRetrySpec)}); err != nil {
		t.Fatalf("create driver: %v", err)
	}
	other := "panel-v"
	if _, err := f.gw.CreateComponent(f.ctx, "", storage.ComponentSpec{Name: other}, f.all, f.all, f.all, f.all); err != nil {
		t.Fatal(err)
	}
	for comp, drv := range map[string]string{f.component: "newtron-nvp-udp", other: "udp-no-retry"} {
		if _, err := f.gw.CreateEndpoint(f.ctx, "", storage.EndpointSpec{Driver: &drv, Component: &comp, Node: &f.node,
			Inputs: map[string]string{"host": "10.0.0.9"}, SecretRead: f.all}, f.all); err != nil {
			t.Fatalf("attach %s: %v", drv, err)
		}
	}
	actor := ownerActor(t, f)
	for _, comp := range []string{f.component, other} {
		if _, err := f.gw.IssueCommand(f.ctx, actor, "component", comp, "set-input", "", json.RawMessage(`"hdmi2"`), nil); err != nil {
			t.Fatalf("issue on %s: %v", comp, err)
		}
	}
	got, err := f.gw.PendingNodeCommands(f.ctx, f.node)
	if err != nil || len(got) != 2 {
		t.Fatalf("pending = %+v (err %v), want both commands", got, err)
	}
	retries := map[string]int{}
	for _, d := range got {
		if d.Transport != "udp" || d.Line != "SET INPUT hdmi2" || d.SessionOnly {
			t.Fatalf("delivery = %+v, want a udp datagram command, never session-only", d)
		}
		retries[d.Target+" "+d.Reply.Prefix] = d.Retries
	}
	if retries["10.0.0.9:51327 INPUT "] != 2 {
		t.Fatalf("the seeded driver's set-input retries = %v, want the 2 it declares (an absolute set is safe to re-send)", retries)
	}
	if retries["10.0.0.9:9100 INPUT "] != 0 {
		t.Fatalf("a binding declaring no retries = %v, want 0: a command is sent once", retries)
	}
}

// ownerActor bootstraps an owner and returns its principal id.
func ownerActor(t *testing.T, f *tlsFixture) string {
	t.Helper()
	_, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.BootstrapOwner(f.ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(context.Background(), f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	var actor string
	if err := conn.QueryRow(context.Background(), `select principal_id from human where username = 'root'`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	return actor
}
