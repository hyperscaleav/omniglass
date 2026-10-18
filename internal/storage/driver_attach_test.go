package storage_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// The driver spec's write gate and the attach flow (#813): a spec that fails
// validation refuses at write, not at collection time; attaching a driver to a
// component authors the endpoint and derives its tasks. Skipped under -short.

// lineProtoSpec is a full three-family spec over tcp, the shape the seed's
// line-protocol driver carries: every referenced name is in the seeded canon.
const lineProtoSpec = `{
	"version": 1,
	"transport": "tcp",
	"inputs": [
		{"name": "host", "kind": "string", "required": true},
		{"name": "port", "kind": "number", "default": "4998"}
	],
	"polls": [{
		"name": "status",
		"schedule": {"every": "30s"},
		"request": {"line": "GET STATUS"},
		"emits": [
			{"name": "video-input", "extract": {"regex": "^INPUT (\\S+)$"}}
		]
	}],
	"listeners": [{
		"name": "events",
		"arm": ["SUBSCRIBE EVENTS"],
		"match": {"prefix": "EVT "},
		"emits": [
			{"name": "video-input", "extract": {"regex": "^EVT INPUT (\\S+)$"}}
		]
	}],
	"commands": [
		{"command_type": "set-input", "request": {"line": "SET INPUT ${value}"}}
	]
}`

// snmpSpec rides snmp with a secret-reference input, the seed's snmp-generic
// shape thinned to one poll.
const snmpSpec = `{
	"version": 1,
	"transport": "snmp",
	"inputs": [
		{"name": "host", "kind": "string", "required": true},
		{"name": "port", "kind": "number", "default": "161"},
		{"name": "community", "kind": "secret", "secret_type": "snmp-community", "required": true}
	],
	"polls": [{
		"name": "scalars",
		"schedule": {"every": "60s"},
		"request": {"get": ["1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.3.0"]},
		"emits": [
			{"name": "model-number", "extract": {"oid": "1.3.6.1.2.1.1.1.0"}},
			{"name": "uptime", "extract": {"oid": "1.3.6.1.2.1.1.3.0"}, "transform": {"scale": 0.01}}
		]
	}]
}`

func TestDriverSpecRefusesAtWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// An unregistered emitted name refuses at write, naming the stranger.
	bad := strings.Replace(lineProtoSpec, `"video-input"`, `"warp-factor"`, 1)
	_, err = gw.CreateDriver(ctx, "", storage.Driver{Name: "bad-driver", Label: "Bad", Version: "1.0.0", Spec: []byte(bad)})
	if err == nil || !strings.Contains(err.Error(), "warp-factor") {
		t.Fatalf("unregistered emitted name at create: err = %v, want a refusal naming warp-factor", err)
	}
	if !errors.Is(err, storage.ErrSpecInvalid) {
		t.Fatalf("refusal is not ErrSpecInvalid: %v", err)
	}

	// A syntactically broken spec refuses too (unknown field: reject, not project).
	_, err = gw.CreateDriver(ctx, "", storage.Driver{Name: "typo-driver", Label: "Typo", Version: "1.0.0", Spec: []byte(`{"version":1,"transport":"tcp","pols":[]}`)})
	if !errors.Is(err, storage.ErrSpecInvalid) {
		t.Fatalf("unknown field at create: err = %v, want ErrSpecInvalid", err)
	}

	// A valid spec writes, and round-trips on the read side.
	d, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "line-proto", Label: "Line Proto", Version: "1.0.0", Spec: []byte(lineProtoSpec)})
	if err != nil {
		t.Fatalf("create with valid spec: %v", err)
	}
	got, err := gw.GetDriver(ctx, d.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Spec) == 0 || !strings.Contains(string(got.Spec), "SUBSCRIBE EVENTS") {
		t.Fatalf("spec did not round-trip: %s", got.Spec)
	}

	// The update gate is the same door: a patch to a broken spec refuses.
	badSpec := []byte(`{"version": 7}`)
	if _, err := gw.UpdateDriver(ctx, "", d.ID, storage.DriverPatch{Spec: badSpec}); !errors.Is(err, storage.ErrSpecInvalid) {
		t.Fatalf("update to a broken spec: err = %v, want ErrSpecInvalid", err)
	}
}

func TestAttachDriverAuthorsTheEndpointAndDerivesTasks(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t), storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "amp-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "lab-community", SecretType: "snmp-community", OwnerKind: "platform",
		Fields: map[string]string{"community": "public"},
	}, all, true); err != nil {
		t.Fatalf("create secret: %v", err)
	}
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "snmp-scalar", Label: "SNMP Scalar", Version: "1.0.0", Spec: []byte(snmpSpec)}); err != nil {
		t.Fatalf("create driver: %v", err)
	}

	comp := "amp-1"
	drv := "snmp-scalar"

	// Attaching with inputs (host; community from a secret) derives everything:
	// no transport typed, no tasks authored.
	ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver:     &drv,
		Component:  &comp,
		Inputs:     map[string]string{"host": "10.20.4.40", "community": "lab-community"},
		SecretRead: all,
		CanAdmin:   true,
	}, all)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if ep.Transport != "snmp" || ep.Name != "snmp" {
		t.Fatalf("attach derived transport %q name %q, want snmp/snmp (from the spec)", ep.Transport, ep.Name)
	}
	if ep.Driver == nil || *ep.Driver != "snmp-scalar" {
		t.Fatalf("endpoint driver = %v, want snmp-scalar", ep.Driver)
	}
	var params map[string]any
	if err := json.Unmarshal(ep.Params, &params); err != nil {
		t.Fatalf("params: %v", err)
	}
	if params["target"] != "10.20.4.40:161" {
		t.Fatalf("params target = %v, want host:defaulted-port", params["target"])
	}
	var inputs map[string]string
	if err := json.Unmarshal(ep.Inputs, &inputs); err != nil {
		t.Fatalf("inputs: %v", err)
	}
	if inputs["community"] != "lab-community" {
		t.Fatalf("inputs = %v, want the secret reference kept by name", inputs)
	}

	// Task derivation: the reachability probe plus one poll task per poll
	// function, its spec baked (function, schedule, request, emits with their
	// lanes resolved at attach, not at collection).
	tasks, err := gw.ListTasks(ctx, all)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	var polls []storage.Task
	for _, task := range tasks {
		if task.EndpointID == ep.ID && task.Mode == "poll" && len(task.Spec) > 2 {
			polls = append(polls, task)
		}
	}
	if len(polls) != 1 {
		t.Fatalf("driver poll tasks = %d, want exactly 1 (the scalars function)", len(polls))
	}
	var baked struct {
		Driver   string `json:"driver"`
		Function string `json:"function"`
		Schedule struct {
			Every string `json:"every"`
		} `json:"schedule"`
		Emits []struct {
			Name string `json:"name"`
			Lane string `json:"lane"`
		} `json:"emits"`
	}
	if err := json.Unmarshal(polls[0].Spec, &baked); err != nil {
		t.Fatalf("baked spec: %v", err)
	}
	if baked.Driver != "snmp-scalar" || baked.Function != "scalars" || baked.Schedule.Every != "60s" {
		t.Fatalf("baked = %+v, want the scalars function carried whole", baked)
	}
	lanes := map[string]string{}
	for _, em := range baked.Emits {
		lanes[em.Name] = em.Lane
	}
	if lanes["uptime"] != "metric" || lanes["model-number"] != "property" {
		t.Fatalf("baked lanes = %v, want uptime:metric model-number:property", lanes)
	}

	// Attach is idempotent-shaped like every derived write: a second attach of
	// the same driver on the same component is the usual one-per-transport 409.
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &drv, Component: &comp,
		Inputs:     map[string]string{"host": "10.20.4.40", "community": "lab-community"},
		SecretRead: all, CanAdmin: true,
	}, all); !errors.Is(err, storage.ErrEndpointExists) {
		t.Fatalf("second attach: err = %v, want ErrEndpointExists", err)
	}
}

func TestAttachDriverListenerDerivesAStandingTask(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "switcher-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "line-proto", Label: "Line Proto", Version: "1.0.0", Spec: []byte(lineProtoSpec)}); err != nil {
		t.Fatalf("create driver: %v", err)
	}
	comp, drv := "switcher-1", "line-proto"
	ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &drv, Component: &comp,
		Inputs: map[string]string{"host": "10.20.4.50"},
	}, all)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}

	tasks, err := gw.ListTasks(ctx, all)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	modes := map[string]int{}
	var listen storage.Task
	for _, task := range tasks {
		if task.EndpointID != ep.ID {
			continue
		}
		modes[task.Mode]++
		if task.Mode == "listen" {
			listen = task
		}
	}
	// The reachability probe and the status poll, plus the standing listen task.
	if modes["poll"] != 2 || modes["listen"] != 1 {
		t.Fatalf("derived tasks by mode = %v, want poll:2 listen:1", modes)
	}
	if !strings.Contains(string(listen.Spec), `"events"`) || !strings.Contains(string(listen.Spec), "SUBSCRIBE EVENTS") {
		t.Fatalf("listen task spec = %s, want the events listener carried whole", listen.Spec)
	}
}

func TestAttachDriverRefusals(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t), storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "amp-2"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "wrong-shape", SecretType: "basic-auth", OwnerKind: "platform",
		Fields: map[string]string{"username": "u", "password": "p"},
	}, all, true); err != nil {
		t.Fatalf("create secret: %v", err)
	}
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "snmp-scalar", Label: "SNMP Scalar", Version: "1.0.0", Spec: []byte(snmpSpec)}); err != nil {
		t.Fatalf("create driver: %v", err)
	}
	comp, drv := "amp-2", "snmp-scalar"

	attach := func(inputs map[string]string) error {
		_, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
			Driver: &drv, Component: &comp, Inputs: inputs,
			SecretRead: all, CanAdmin: true,
		}, all)
		return err
	}

	cases := []struct {
		name   string
		inputs map[string]string
		want   string
	}{
		{"missing required input", map[string]string{"community": "lab-community"}, "host"},
		{"input the spec does not declare", map[string]string{"host": "h", "community": "lab-community", "warp": "9"}, "warp"},
		{"secret reference to a missing row", map[string]string{"host": "h", "community": "no-such-secret"}, "does not exist or is not yours"},
		{"secret reference of the wrong shape", map[string]string{"host": "h", "community": "wrong-shape"}, "snmp-community"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := attach(tc.inputs)
			if !errors.Is(err, storage.ErrAttachInvalid) {
				t.Fatalf("err = %v, want ErrAttachInvalid", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refusal %q does not name %q", err, tc.want)
			}
		})
	}

	// A name two secrets share refuses rather than guessing: the reference is
	// stored by name, and guessing could bind another tenant's credential.
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "dup-secret", SecretType: "snmp-community", OwnerKind: "platform",
		Fields: map[string]string{"community": "one"},
	}, all, true); err != nil {
		t.Fatalf("create dup-secret 1: %v", err)
	}
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "dup-secret", SecretType: "snmp-community", OwnerKind: "component", OwnerName: strPtr("amp-2"),
		Fields: map[string]string{"community": "two"},
	}, all, true); err != nil {
		t.Fatalf("create dup-secret 2: %v", err)
	}
	if err := attach(map[string]string{"host": "h", "community": "dup-secret"}); !errors.Is(err, storage.ErrAttachInvalid) || !strings.Contains(err.Error(), "more than one secret you can use") {
		t.Fatalf("ambiguous secret reference: err = %v, want ErrAttachInvalid naming the collision", err)
	}

	// An unknown driver reference is the registry's not-found, mapped by the
	// API tier to a 422 like the other reference faults.
	ghost := "no-such-driver"
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &ghost, Component: &comp, Inputs: map[string]string{"host": "h"},
	}, all); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("unknown driver: err = %v, want ErrTypeNotFound", err)
	}

	// A driverless create still works exactly as before: the bare probe path.
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Transport: "tcp", Component: &comp, Params: []byte(`{"target":"10.0.0.7:5000"}`),
	}, all); err != nil {
		t.Fatalf("bare create alongside attach: %v", err)
	}

	// Driver and transport together contradict: the transport is the spec's fact.
	tr := "tcp"
	_ = tr
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &drv, Transport: "tcp", Component: &comp, Inputs: map[string]string{"host": "h", "community": "lab-community"},
		SecretRead: all, CanAdmin: true,
	}, all); !errors.Is(err, storage.ErrAttachInvalid) {
		t.Fatalf("driver+transport: err = %v, want ErrAttachInvalid", err)
	}
}

// TestNodeWorklistDeliversSecretInputs pins the credential delivery contract
// (#814): a driver task whose spec declares a secret input reaches its placed
// node with that input's fields unsealed (the node must present the community
// to the device), while the reachability task and non-driver tasks carry none.
// Delivery rides the per-node worklist subject, whose NATS grant is the
// isolation boundary; the attach audit row is the record of the binding.
func TestNodeWorklistDeliversSecretInputs(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t), storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "amp-3"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-3"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "lab-community", SecretType: "snmp-community", OwnerKind: "platform",
		Fields: map[string]string{"community": "very-secret"},
	}, all, true); err != nil {
		t.Fatalf("create secret: %v", err)
	}
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "snmp-scalar", Label: "SNMP Scalar", Version: "1.0.0", Spec: []byte(snmpSpec)}); err != nil {
		t.Fatalf("create driver: %v", err)
	}
	comp, drv, node := "amp-3", "snmp-scalar", "edge-3"
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &drv, Component: &comp, Node: &node,
		Inputs:     map[string]string{"host": "10.20.4.40", "community": "lab-community"},
		SecretRead: all, CanAdmin: true,
	}, all); err != nil {
		t.Fatalf("attach: %v", err)
	}

	wl, err := gw.NodeWorklist(ctx, "edge-3")
	if err != nil {
		t.Fatalf("worklist: %v", err)
	}
	var driverTask, reachTask *storage.WorklistTask
	for i := range wl.Tasks {
		if len(wl.Tasks[i].Spec) > 2 {
			driverTask = &wl.Tasks[i]
		} else {
			reachTask = &wl.Tasks[i]
		}
	}
	if driverTask == nil || reachTask == nil {
		t.Fatalf("worklist tasks = %+v, want the driver poll and the reachability probe", wl.Tasks)
	}
	if got := driverTask.Secrets["community"]["community"]; got != "very-secret" {
		t.Fatalf("driver task secrets = %v, want the community field unsealed", driverTask.Secrets)
	}
	if len(reachTask.Secrets) != 0 {
		t.Fatalf("the reachability task carries secrets it has no use for: %v", reachTask.Secrets)
	}
}

// TestNodeWorklistPropagatesUnsealFailure pins the other half of the delivery
// contract: a driver task references a real, in-scope secret, but the credential
// can no longer be unsealed (here the key provider cannot open the envelope,
// standing in for a provider outage or envelope corruption). That is NOT
// absence, so the worklist pull FAILS rather than delivering a phantom-absent
// credential. Swallowing it would mask an infra fault as a missing secret and
// send operators chasing a credential that exists. The node keeps its last-good
// worklist on a failed pull and retries.
func TestNodeWorklistPropagatesUnsealFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "amp-3"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-3"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "lab-community", SecretType: "snmp-community", OwnerKind: "platform",
		Fields: map[string]string{"community": "very-secret"},
	}, all, true); err != nil {
		t.Fatalf("create secret: %v", err)
	}
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "snmp-scalar", Label: "SNMP Scalar", Version: "1.0.0", Spec: []byte(snmpSpec)}); err != nil {
		t.Fatalf("create driver: %v", err)
	}
	comp, drv, node := "amp-3", "snmp-scalar", "edge-3"
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &drv, Component: &comp, Node: &node,
		Inputs:     map[string]string{"host": "10.20.4.40", "community": "lab-community"},
		SecretRead: all, CanAdmin: true,
	}, all); err != nil {
		t.Fatalf("attach: %v", err)
	}
	gw.Close()

	// Reopen on the same database with a DIFFERENT provider key: the sealed
	// community can no longer be opened, a real unseal failure (AES-GCM refuses
	// the wrong key), distinct from a secret that resolves to nothing.
	bad, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x9}, 32))))
	if err != nil {
		t.Fatalf("reopen gateway: %v", err)
	}
	defer bad.Close()
	if _, err := bad.NodeWorklist(ctx, "edge-3"); err == nil {
		t.Fatalf("NodeWorklist succeeded despite an unopenable credential; a real unseal failure must not be masked as an absent secret")
	}
}

// TestCommandWireDelivery pins the storage side of the actuation path (#815):
// a node's pending queue resolves the commands its placed endpoints can
// actuate, renders the binding's request against the intended value, marks
// them dispatched (redelivered only after silence), and records the execution
// report once, under the same per-node confinement the worklist carries.
func TestCommandWireDelivery(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "switcher-9"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	for _, n := range []string{"edge-9", "edge-idle"} {
		if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: n}, all, all); err != nil {
			t.Fatalf("create node %s: %v", n, err)
		}
	}
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "line-proto", Label: "Line Proto", Version: "1.0.0", Spec: []byte(lineProtoSpec)}); err != nil {
		t.Fatalf("create driver: %v", err)
	}
	comp, drv, node := "switcher-9", "line-proto", "edge-9"
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &drv, Component: &comp, Node: &node,
		Inputs: map[string]string{"host": "10.20.4.50"},
	}, all); err != nil {
		t.Fatalf("attach: %v", err)
	}

	_, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	conn0, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	var actor string
	if err := conn0.QueryRow(ctx, `select principal_id from human where username = 'root'`).Scan(&actor); err != nil {
		t.Fatalf("resolve actor: %v", err)
	}
	conn0.Close(ctx)

	cmd, err := gw.IssueCommand(ctx, actor, "component", "switcher-9", "set-input", "", json.RawMessage(`"hdmi-2"`), nil)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if cmd.Status != "issued" {
		t.Fatalf("status = %q, want issued (settleable, unsettled)", cmd.Status)
	}

	// A node the endpoint is NOT placed on sees nothing.
	if got, err := gw.PendingNodeCommands(ctx, "edge-idle"); err != nil || len(got) != 0 {
		t.Fatalf("idle node queue = %v (err %v), want empty", got, err)
	}

	// The placed node gets the rendered actuation, once.
	got, err := gw.PendingNodeCommands(ctx, "edge-9")
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(got) != 1 {
		connD, errD := pgx.Connect(ctx, dsn)
		if errD == nil {
			var st, execErr *string
			var disp, exec *time.Time
			_ = connD.QueryRow(ctx, `select status, exec_error, dispatched_at, executed_at from command where id = $1`, cmd.ID).Scan(&st, &execErr, &disp, &exec)
			connD.Close(ctx)
			t.Fatalf("queue = %+v, want the one command (row: status=%v exec_error=%v dispatched=%v executed=%v)", got, deref(st), deref(execErr), disp, exec)
		}
		t.Fatalf("queue = %+v, want the one command", got)
	}
	d := got[0]
	if d.ID != cmd.ID || d.CommandType != "set-input" || d.Transport != "tcp" {
		t.Fatalf("delivery = %+v", d)
	}
	if d.Line != "SET INPUT hdmi-2" {
		t.Fatalf("rendered line = %q, want the intended value substituted", d.Line)
	}
	if d.Target != "10.20.4.50:4998" {
		t.Fatalf("target = %q, want the endpoint's derived target", d.Target)
	}

	// Dispatched: an immediate re-pull redelivers nothing (at-least-once
	// means silence redelivers, not every pull).
	if again, err := gw.PendingNodeCommands(ctx, "edge-9"); err != nil || len(again) != 0 {
		t.Fatalf("immediate re-pull = %v (err %v), want empty", again, err)
	}

	// Another node cannot stamp the execution: a report from a node the command
	// was not dispatched to is rejected (not a silent no-op), so a stray or
	// forged status cannot close a command a different node owns.
	if err := gw.RecordCommandExecution(ctx, "edge-idle", cmd.ID, ""); !errors.Is(err, storage.ErrCommandNotDispatchedHere) {
		t.Fatalf("record from wrong node: err = %v, want ErrCommandNotDispatchedHere", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var executed *time.Time
	row := func() *time.Time {
		var ts *time.Time
		if err := conn.QueryRow(ctx, `select executed_at from command where id = $1`, cmd.ID).Scan(&ts); err != nil {
			t.Fatalf("read executed_at: %v", err)
		}
		return ts
	}
	if executed = row(); executed != nil {
		t.Fatalf("wrong node stamped execution")
	}
	if err := gw.RecordCommandExecution(ctx, "edge-9", cmd.ID, ""); err != nil {
		t.Fatalf("record: %v", err)
	}
	if executed = row(); executed == nil {
		t.Fatal("execution not stamped")
	}
	first := *executed
	if err := gw.RecordCommandExecution(ctx, "edge-9", cmd.ID, "late duplicate"); !errors.Is(err, storage.ErrCommandNotDispatchedHere) {
		t.Fatalf("re-record: err = %v, want ErrCommandNotDispatchedHere (already stamped)", err)
	}
	if executed = row(); !executed.Equal(first) {
		t.Fatal("a redelivered report re-stamped the execution")
	}

	// A command no driver binds (reboot is not in the line-proto spec) is
	// never delivered; its arc is settlement's business alone.
	if _, err := gw.IssueCommand(ctx, actor, "component", "switcher-9", "reboot", "", nil, nil); err != nil {
		t.Fatalf("issue reboot: %v", err)
	}
	if got, err := gw.PendingNodeCommands(ctx, "edge-9"); err != nil || len(got) != 0 {
		t.Fatalf("unbound command delivered: %v (err %v)", got, err)
	}
}

// TestAttachScopesSecretReference is the authz regression for the rollup review:
// an attach may only bind a secret the CALLER could themselves read, so an
// out-of-scope secret and an admin-sensitive one (without the admin tier) both
// refuse indistinguishably from an absent one, with no existence-or-type oracle.
func TestAttachScopesSecretReference(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t), storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "west-amp"}, all, all, all, all); err != nil {
		t.Fatalf("create west-amp: %v", err)
	}
	// A platform community secret (in reach only under an all secret-read scope)
	// and a platform admin-sensitive one.
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "east-community", SecretType: "snmp-community", OwnerKind: "platform",
		Fields: map[string]string{"community": "east-secret"},
	}, all, true); err != nil {
		t.Fatalf("create platform secret: %v", err)
	}
	adminSensitive := true
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "priv-community", SecretType: "snmp-community", OwnerKind: "platform",
		AdminSensitive: &adminSensitive,
		Fields:         map[string]string{"community": "privileged"},
	}, all, true); err != nil {
		t.Fatalf("create admin-sensitive secret: %v", err)
	}
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "snmp-scalar", Label: "SNMP Scalar", Version: "1.0.0", Spec: []byte(snmpSpec)}); err != nil {
		t.Fatalf("create driver: %v", err)
	}

	// An empty secret-read scope admits nothing, standing in for an operator
	// whose secret reach does not cover this credential.
	noReach := scope.Set{}
	comp, drv := "west-amp", "snmp-scalar"
	attachAs := func(ref string, secretRead scope.Set, canAdmin bool) error {
		_, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
			Driver: &drv, Component: &comp,
			Inputs:     map[string]string{"host": "h", "community": ref},
			SecretRead: secretRead, CanAdmin: canAdmin,
		}, all)
		return err
	}

	// An out-of-scope secret refuses with the SAME message as an absent one:
	// no existence oracle for the west operator over east's secrets.
	errOut := attachAs("east-community", noReach, false)
	errAbsent := attachAs("no-such", noReach, false)
	if !errors.Is(errOut, storage.ErrAttachInvalid) || !strings.Contains(errOut.Error(), "does not exist or is not yours") {
		t.Fatalf("out-of-scope secret: err = %v, want the non-disclosing refusal", errOut)
	}
	if errOut.Error() != errAbsent.Error() {
		t.Fatalf("out-of-scope refusal differs from absent (%q vs %q): an oracle", errOut, errAbsent)
	}

	// An admin-sensitive secret is invisible without the admin tier, and the
	// refusal never names its type either.
	if err := attachAs("priv-community", all, false); !errors.Is(err, storage.ErrAttachInvalid) || strings.Contains(err.Error(), "snmp-community") {
		t.Fatalf("admin-sensitive without tier: err = %v, want the non-disclosing refusal", err)
	}
	// With the admin tier it resolves.
	if err := attachAs("priv-community", all, true); err != nil {
		t.Fatalf("admin-sensitive with tier: %v", err)
	}
}

// TestUpdateEndpointRefusesDriverParams pins the second half of the credential
// confinement (rollup review): a driver-attached endpoint's params are derived
// from its inputs, so endpoint:update cannot repoint them (which would send the
// credential to a target the inputs never named). The re-attach is the write
// path for its address.
func TestUpdateEndpointRefusesDriverParams(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t), storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "amp-8"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "lab-community", SecretType: "snmp-community", OwnerKind: "platform",
		Fields: map[string]string{"community": "public"},
	}, all, true); err != nil {
		t.Fatalf("create secret: %v", err)
	}
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "snmp-scalar", Label: "SNMP Scalar", Version: "1.0.0", Spec: []byte(snmpSpec)}); err != nil {
		t.Fatalf("create driver: %v", err)
	}
	comp, drv := "amp-8", "snmp-scalar"
	ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &drv, Component: &comp,
		Inputs:     map[string]string{"host": "10.20.4.40", "community": "lab-community"},
		SecretRead: all, CanAdmin: true,
	}, all)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	// A params patch on the attached endpoint refuses.
	if _, err := gw.UpdateEndpoint(ctx, "", ep.ID, storage.EndpointPatch{Params: []byte(`{"target":"attacker:161"}`)}, all, all); !errors.Is(err, storage.ErrEndpointDriverParams) {
		t.Fatalf("params patch on attached endpoint: err = %v, want ErrEndpointDriverParams", err)
	}
	// A label-only patch still works (it moves nothing about the address).
	label := "Room amp"
	if _, err := gw.UpdateEndpoint(ctx, "", ep.ID, storage.EndpointPatch{Label: &label}, all, all); err != nil {
		t.Fatalf("label patch on attached endpoint: %v", err)
	}
}
