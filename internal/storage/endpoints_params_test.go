package storage_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// A bare dialed endpoint's params must carry the one key the node dials
// (#1187): host and port are not keys, and params without a target would be
// skipped by the node forever, so they are refused at write, on create and on
// an edit, naming target. udp binds a local port and a push transport is
// dialed by no one, so neither needs one; tls-mode and listen-port stay
// checked as they were. Skipped under -short.
func TestABareDialedEndpointsParamsNameATarget(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, transport, params string
		wantErr                 string // empty: accepted
	}{
		{"tcp host and port", "tcp", `{"host":"127.0.0.1","port":8080}`, "target"},
		{"tcp target", "tcp", `{"target":"127.0.0.1:8080"}`, ""},
		{"tcp target beside a host", "tcp", `{"target":"127.0.0.1:8080","host":"127.0.0.1"}`, "target"},
		{"tcp empty target", "tcp", `{"target":"  "}`, "target"},
		{"tcp target not a string", "tcp", `{"target":8080}`, "target"},
		{"tcp not an object", "tcp", `"127.0.0.1:8080"`, "target"},
		{"icmp empty object", "icmp", `{}`, "target"},
		{"icmp host", "icmp", `{"target":"10.0.0.1","count":3,"timeout":"2s"}`, ""},
		{"ssh no target", "ssh", `{"timeout":"2s"}`, "target"},
		{"snmp no target", "snmp", `{"count":1}`, "target"},
		{"tls target and mode", "tls", `{"target":"10.0.0.1:51326","tls-mode":"pin"}`, ""},
		{"tls no target", "tls", `{"tls-mode":"pin"}`, "target"},
		{"tls bad mode", "tls", `{"target":"10.0.0.1:51326","tls-mode":"pinned"}`, "tls-mode"},
		{"udp listen port, no target", "udp", `{"listen-port":4000}`, ""},
		{"udp bad listen port", "udp", `{"listen-port":99999}`, "listen-port"},
		{"udp empty object", "udp", `{}`, ""},
	}
	for i, tc := range cases {
		comp := fmt.Sprintf("dialed-%d", i)
		mustCreateComponent(t, gw, storage.ComponentSpec{Name: comp}, all)

		_, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: tc.transport, Component: &comp, Params: []byte(tc.params)}, all)
		if tc.wantErr == "" {
			if err != nil {
				t.Errorf("%s: create = %v, want accepted", tc.name, err)
			}
		} else if !errors.Is(err, storage.ErrEndpointParamsInvalid) || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: create = %v, want ErrEndpointParamsInvalid naming %s", tc.name, err, tc.wantErr)
		}

		// The same params on an edit of an endpoint holding a good target.
		editComp := comp + "-edit"
		mustCreateComponent(t, gw, storage.ComponentSpec{Name: editComp}, all)
		good := `{"target":"10.9.9.9:1"}`
		if tc.transport == "udp" {
			good = `{}`
		}
		ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: tc.transport, Component: &editComp, Params: []byte(good)}, all)
		if err != nil {
			t.Fatalf("%s: seed endpoint: %v", tc.name, err)
		}
		_, err = gw.UpdateEndpoint(ctx, "", ep.ID, storage.EndpointPatch{Params: []byte(tc.params)}, all, all)
		if tc.wantErr == "" {
			if err != nil {
				t.Errorf("%s: edit = %v, want accepted", tc.name, err)
			}
		} else if !errors.Is(err, storage.ErrEndpointPatchInvalid) || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: edit = %v, want ErrEndpointPatchInvalid naming %s", tc.name, err, tc.wantErr)
		}
	}

	// An edit that leaves the params alone is not held to the check.
	keep := "dialed-keep"
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: keep}, all)
	ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "tcp", Component: &keep, Params: []byte(`{"target":"10.9.9.9:1"}`)}, all)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.UpdateEndpoint(ctx, "", ep.ID, storage.EndpointPatch{}, all, all); err != nil {
		t.Errorf("an edit without params = %v, want accepted", err)
	}
}
