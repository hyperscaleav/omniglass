package storage_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
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
		{"command_type": "set-input", "request": {"line": "SET INPUT ${arg.input}"}}
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
		Driver:    &drv,
		Component: &comp,
		Inputs:    map[string]string{"host": "10.20.4.40", "community": "lab-community"},
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
		Inputs: map[string]string{"host": "10.20.4.40", "community": "lab-community"},
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
		{"secret reference to a missing row", map[string]string{"host": "h", "community": "no-such-secret"}, "no-such-secret"},
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
	}, all); !errors.Is(err, storage.ErrAttachInvalid) {
		t.Fatalf("driver+transport: err = %v, want ErrAttachInvalid", err)
	}
}
