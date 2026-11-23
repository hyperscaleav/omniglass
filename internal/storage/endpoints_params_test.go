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
	"github.com/jackc/pgx/v5"
)

// A bare dialed endpoint's params must carry the one key the node dials
// (#1187): host and port are not keys (the address goes under target as
// host[:port]), and params without a target would be skipped by the node
// forever, so they are refused at write, on create and on an edit, naming
// target. The target needs a port except on icmp, which dials none, and snmp,
// which defaults to 161: a bare host elsewhere would dial nothing. udp binds a local
// port and a push transport is dialed by no one, so neither needs one;
// tls-mode and listen-port stay checked as they were. Skipped under -short.
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
		{"tcp target missing port", "tcp", `{"target":"10.0.0.1"}`, "target"},
		{"tcp bad timeout", "tcp", `{"target":"10.0.0.1:8080","timeout":"five minutes"}`, "timeout"},
		{"tcp zero timeout", "tcp", `{"target":"10.0.0.1:8080","timeout":"0s"}`, "timeout"},
		{"tcp good timeout", "tcp", `{"target":"10.0.0.1:8080","timeout":"30s"}`, ""},
		{"ssh target missing port", "ssh", `{"target":"10.0.0.9"}`, "target"},
		{"snmp bare host defaults to 161", "snmp", `{"target":"10.0.0.9"}`, ""},
		{"icmp empty object", "icmp", `{}`, "target"},
		{"icmp host", "icmp", `{"target":"10.0.0.1","count":3,"timeout":"2s"}`, ""},
		{"icmp bad timeout", "icmp", `{"target":"10.0.0.1","timeout":"five minutes"}`, "timeout"},
		{"icmp count not a number", "icmp", `{"target":"10.0.0.1","count":"three"}`, "count"},
		{"icmp count negative", "icmp", `{"target":"10.0.0.1","count":-1}`, "count"},
		{"icmp count not integer", "icmp", `{"target":"10.0.0.1","count":1.5}`, "count"},
		{"icmp count spelled as a float", "icmp", `{"target":"10.0.0.1","count":3.0}`, "count"},
		{"ssh no target", "ssh", `{"timeout":"2s"}`, "target"},
		{"snmp no target", "snmp", `{"count":1}`, "target"},
		{"tls target and mode", "tls", `{"target":"10.0.0.1:51326","tls-mode":"pin"}`, ""},
		{"tls no target", "tls", `{"tls-mode":"pin"}`, "target"},
		{"tls bad mode", "tls", `{"target":"10.0.0.1:51326","tls-mode":"pinned"}`, "tls-mode"},
		{"udp listen port, no target", "udp", `{"listen-port":4000}`, ""},
		{"udp bad listen port", "udp", `{"listen-port":99999}`, "listen-port"},
		{"udp bad timeout", "udp", `{"listen-port":4000,"timeout":"five minutes"}`, "timeout"},
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

// Endpoints stored before the write-time target check (#1187) are named by the
// boot audit (#2249): host and port keys, a bare host on a transport with no
// default port. Rows that pass the check are not named, and nothing is rewritten.
func TestStoredEndpointTargetViolations(t *testing.T) {
	t.Parallel()
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
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(ctx) })

	plant := []struct{ name, transport, params string }{
		{"hostport", "tcp", `{"host":"10.0.0.1","port":8080}`},
		{"barehost", "tcp", `{"target":"10.0.0.1"}`},
		{"okaytcp", "tcp", `{"target":"10.0.0.1:8080"}`},
		{"okaysnmp", "snmp", `{"target":"10.0.0.1"}`},
		{"okayicmp", "icmp", `{"target":"10.0.0.1"}`},
		{"okayudp", "udp", `{"listen-port":4000}`},
	}
	for _, e := range plant {
		mustCreateComponent(t, gw, storage.ComponentSpec{Name: "c-" + e.name}, all)
		if _, err := conn.Exec(ctx, `insert into endpoint (name, transport, component, params)
			select $1, $2, id, $3::jsonb from component where name = $4`, e.name, e.transport, e.params, "c-"+e.name); err != nil {
			t.Fatalf("plant %s: %v", e.name, err)
		}
	}

	got, err := gw.StoredEndpointTargetViolations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Endpoint != "c-barehost/barehost" || got[1].Endpoint != "c-hostport/hostport" {
		t.Fatalf("violations = %+v, want barehost and hostport only", got)
	}
	if !strings.Contains(got[1].Problem, "target") {
		t.Errorf("problem %q does not name target", got[1].Problem)
	}
	var params string
	if err := conn.QueryRow(ctx, `select params::text from endpoint where name = 'hostport'`).Scan(&params); err != nil || !strings.Contains(params, "host") {
		t.Errorf("the audit rewrote a row: %q %v", params, err)
	}
}
