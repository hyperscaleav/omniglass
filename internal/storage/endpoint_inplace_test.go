package storage_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// A driver-attached endpoint's connection inputs edit in place (the
// architect's ruling on #1061): the host, port and attached secret
// references through the same update, validated against the driver's
// declared inputs, scoped, audited in the transaction, and the driver tasks
// re-derived there (ids moving only where the derived spec does). Re-attach
// is not the path.
func TestADriverEndpointEditsItsConnectionInputsInPlace(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	comp, drv := "panel-e", "newtron-nvp-telnet"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: comp}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"panel-login", "other-login"} {
		if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
			Name: name, SecretType: "basic-auth", OwnerKind: "platform",
			Fields: map[string]string{"username": "admin", "password": "newtron"},
		}, all, true); err != nil {
			t.Fatal(err)
		}
	}
	ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &drv, Component: &comp,
		Inputs:     map[string]string{"host": "10.20.4.70", "port": "2399", "login": "panel-login"},
		SecretRead: all, CanAdmin: true,
	}, all)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	taskIDs := func() []string {
		t.Helper()
		rows, err := conn.Query(ctx, `select id from task where endpoint_id = $1 order by id`, ep.ID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var ids []string
		for rows.Next() {
			var id string
			_ = rows.Scan(&id)
			ids = append(ids, id)
		}
		sort.Strings(ids)
		return ids
	}
	before := taskIDs()
	patch := func(p storage.EndpointPatch) (*storage.Endpoint, error) {
		p.SecretRead, p.CanAdmin = all, true
		return gw.UpdateEndpoint(ctx, "", ep.ID, p, all, all)
	}

	// The port, in place: the derived target moves, the stored inputs move,
	// the tasks (derived from the spec, not the address) keep their ids.
	after, err := patch(storage.EndpointPatch{Inputs: map[string]string{"port": "23"}})
	if err != nil {
		t.Fatalf("edit the port in place: %v", err)
	}
	var params, inputs map[string]string
	_ = json.Unmarshal(after.Params, &params)
	_ = json.Unmarshal(after.Inputs, &inputs)
	if params["target"] != "10.20.4.70:23" || inputs["port"] != "23" || inputs["login"] != "panel-login" {
		t.Fatalf("after the port edit params %v inputs %v", params, inputs)
	}
	if got := taskIDs(); strings.Join(got, ",") != strings.Join(before, ",") {
		t.Fatalf("an address edit moved task ids: %v -> %v", before, got)
	}
	// The address and the attached secret, in place; the timeout too.
	if after, err = patch(storage.EndpointPatch{Inputs: map[string]string{"host": "10.20.4.71", "login": "other-login"}, Params: []byte(`{"timeout":"3s"}`)}); err != nil {
		t.Fatalf("edit the address and the secret: %v", err)
	}
	_ = json.Unmarshal(after.Params, &params)
	if params["target"] != "10.20.4.71:23" || params["timeout"] != "3s" {
		t.Fatalf("params = %v", params)
	}
	var n int
	if err := conn.QueryRow(ctx, `select count(*) from audit_log where resource = 'endpoint' and verb = 'update' and resource_id = $1`, ep.ID).Scan(&n); err != nil || n != 2 {
		t.Fatalf("update audit rows = %d (%v), want 2", n, err)
	}

	// Refusals name the field: a value the driver refuses, an undeclared
	// input, a transport the driver does not speak, a raw target, and a
	// secret the caller cannot read (an edit that would send a credential
	// somewhere new needs the caller to be able to use it).
	refusals := []struct {
		patch storage.EndpointPatch
		want  string
		read  scope.Set
	}{
		{storage.EndpointPatch{Inputs: map[string]string{"port": "twenty-three"}}, "port", all},
		{storage.EndpointPatch{Inputs: map[string]string{"warp": "9"}}, "warp", all},
		{storage.EndpointPatch{Transport: strPtr("ssh")}, "transport", all},
		{storage.EndpointPatch{Params: []byte(`{"target":"attacker:23"}`)}, "target", all},
		{storage.EndpointPatch{Inputs: map[string]string{"host": "10.20.4.99"}}, "login", scope.Set{}},
		// Review: the timeout's value is checked, not only its key; a
		// required input cannot be emptied.
		{storage.EndpointPatch{Params: []byte(`{"timeout":"five seconds"}`)}, "params.timeout", all},
		{storage.EndpointPatch{Params: []byte(`{"timeout":5}`)}, "params.timeout", all},
		{storage.EndpointPatch{Inputs: map[string]string{"host": ""}}, "host", all},
	}
	for _, r := range refusals {
		r.patch.SecretRead, r.patch.CanAdmin = r.read, true
		_, err := gw.UpdateEndpoint(ctx, "", ep.ID, r.patch, all, all)
		if !errors.Is(err, storage.ErrEndpointPatchInvalid) || !strings.Contains(err.Error(), r.want) {
			t.Errorf("patch %+v = %v, want ErrEndpointPatchInvalid naming %q", r.patch, err, r.want)
		}
	}
	// The same transport is no change, and is accepted.
	if _, err := patch(storage.EndpointPatch{Transport: strPtr("telnet")}); err != nil {
		t.Fatalf("restating the transport: %v", err)
	}
}

// Review of the in-place edit: an input the driver no longer declares (a
// driver update dropped it) never blocks an edit, and an optional input
// cleared with an empty value falls back to its default.
func TestAnInPlaceEditSurvivesADriverDroppingAnInput(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	spec := func(extra string) []byte {
		return []byte(`{"version":1,"transport":"tcp","inputs":[{"name":"host","kind":"string","required":true},{"name":"port","kind":"number","default":"4998"}` + extra + `],
			"polls":[{"name":"status","schedule":{"every":"30s"},"request":{"line":"GET STATUS"},"emits":[{"name":"video-input","extract":{"regex":"^INPUT (\\S+)$"}}]}]}`)
	}
	d, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "drop-drv", Label: "Drop", Version: "1.0.0", Spec: spec(`,{"name":"note","kind":"string"}`)})
	if err != nil {
		t.Fatal(err)
	}
	comp, drv := "amp-d", "drop-drv"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: comp}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Driver: &drv, Component: &comp,
		Inputs: map[string]string{"host": "10.0.0.3", "port": "5000", "note": "x"}, SecretRead: all, CanAdmin: true}, all)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.UpdateDriver(ctx, "", d.ID, storage.DriverPatch{Spec: spec("")}); err != nil {
		t.Fatal(err)
	}
	after, err := gw.UpdateEndpoint(ctx, "", ep.ID, storage.EndpointPatch{Params: []byte(`{"timeout":"3s"}`), SecretRead: all, CanAdmin: true}, all, all)
	if err != nil {
		t.Fatalf("an edit after the driver dropped an input: %v", err)
	}
	if strings.Contains(string(after.Inputs), "note") {
		t.Fatalf("the dropped input survived: %s", after.Inputs)
	}
	// An optional input cleared falls back to its default.
	after, err = gw.UpdateEndpoint(ctx, "", ep.ID, storage.EndpointPatch{Inputs: map[string]string{"port": ""}, SecretRead: all, CanAdmin: true}, all, all)
	if err != nil {
		t.Fatalf("clear the port: %v", err)
	}
	if !strings.Contains(string(after.Params), "10.0.0.3:4998") {
		t.Fatalf("a cleared port did not fall back to its default: %s", after.Params)
	}
}
