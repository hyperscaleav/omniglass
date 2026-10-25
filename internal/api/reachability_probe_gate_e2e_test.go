package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// probeGateResp is the part of the reachability read a probe's gate serves
// (#1110): the endpoint's id, no session, and the gate with its reason and
// when it was published.
type probeGateResp struct {
	Endpoints []struct {
		ID        string `json:"id"`
		Endpoint  string `json:"endpoint"`
		Transport string `json:"transport"`
		Session   *struct {
			State string `json:"state"`
		} `json:"session"`
		Rejection *struct {
			Rung   string    `json:"rung"`
			Class  string    `json:"class"`
			Reason string    `json:"reason"`
			TS     time.Time `json:"ts"`
		} `json:"rejection"`
	} `json:"endpoints"`
}

// TestReachabilityServesAProbeGateWithNoHeldSession drives the read the
// console's refusal notice renders for an endpoint gated by its reachability
// probe alone (#1110): a tls certificate refusal, a udp terminal reply, an
// snmp authorizationError and an ssh probe's refused credential, none on an
// endpoint holding a session. Each serves its gate's rung, class, reason and
// publish time with no session beside it, the reason cut as a session's is;
// a gate that lifted serves no reason, however recent the last one. A viewer
// reads the same gate, and its retry is refused (403) where an owner's lands.
// Skipped under -short.
func TestReachabilityServesAProbeGateWithNoHeldSession(t *testing.T) {
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
	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	// Bare probe endpoints: no listen task and no driver declaring a session,
	// so nothing on them holds one.
	if _, err := conn.Exec(ctx, `insert into endpoint (name, transport, component, params) values
		('tls', 'tls', (select id from component where name = 'panel-1'), '{"target":"127.60.0.9:51326","tls-mode":"verify"}'::jsonb),
		('udp', 'udp', (select id from component where name = 'panel-1'), '{"target":"127.60.0.9:51327"}'::jsonb),
		('snmp', 'snmp', (select id from component where name = 'panel-1'), '{"target":"127.60.0.9:161"}'::jsonb),
		('ssh', 'ssh', (select id from component where name = 'panel-1'), '{"target":"127.60.0.9:22"}'::jsonb),
		('tcp', 'tcp', (select id from component where name = 'panel-1'), '{"target":"127.60.0.9:51325"}'::jsonb)`); err != nil {
		t.Fatalf("insert endpoints: %v", err)
	}

	const fingerprint = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	long := "auth: ERR " + strings.Repeat("x", 600)
	at := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Microsecond)
	prop := func(key, instance, value string, ts time.Time) storage.PropertySampleWrite {
		return storage.PropertySampleWrite{OwnerKind: "component", OwnerID: "panel-1", Key: key, Instance: instance, Value: value, Source: instance, TS: ts}
	}
	if err := insertProperties(ctx, gw, []storage.PropertySampleWrite{
		prop("endpoint-rejection", "tls", "responsive certificate "+fingerprint, at),
		prop("endpoint-rejection-reason", "tls", "certificate: the certificate failed verification: x509: certificate signed by unknown authority", at),
		prop("endpoint-rejection", "udp", "responsive auth "+fingerprint, at),
		prop("endpoint-rejection-reason", "udp", long, at),
		prop("endpoint-rejection", "snmp", "authenticated auth "+fingerprint, at),
		prop("endpoint-rejection-reason", "snmp", "auth: the agent answered authorizationError for the community", at),
		prop("endpoint-rejection", "ssh", "authenticated auth "+fingerprint, at),
		prop("endpoint-rejection-reason", "ssh", "auth: the component refused the probe's credential", at),
		// A gate that lifted: its reason is the latest one, and still not served.
		prop("endpoint-rejection", "tcp", "responsive protocol "+fingerprint, at.Add(-time.Hour)),
		prop("endpoint-rejection-reason", "tcp", "protocol: the component refused the declared channel", at.Add(-time.Hour)),
		prop("endpoint-rejection", "tcp", "none", at),
	}); err != nil {
		t.Fatalf("insert gate samples: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	read := func(tok string) probeGateResp {
		t.Helper()
		out := c.do(tok, http.MethodGet, "/components/panel-1/reachability", nil, http.StatusOK)
		if strings.Contains(string(out), fingerprint) {
			t.Fatalf("the read served the connection fingerprint: %s", out)
		}
		var r probeGateResp
		if err := json.Unmarshal(out, &r); err != nil {
			t.Fatalf("decode reachability: %v", err)
		}
		return r
	}

	want := map[string]struct{ rung, class, reason string }{
		"tls":  {"responsive", "certificate", "certificate: the certificate failed verification: x509: certificate signed by unknown authority"},
		"udp":  {"responsive", "auth", string([]rune(long)[:500]) + "…"},
		"snmp": {"authenticated", "auth", "auth: the agent answered authorizationError for the community"},
		"ssh":  {"authenticated", "auth", "auth: the component refused the probe's credential"},
	}
	check := func(who string, r probeGateResp) map[string]string {
		t.Helper()
		ids := map[string]string{}
		for _, e := range r.Endpoints {
			ids[e.Endpoint] = e.ID
			if e.Session != nil {
				t.Errorf("%s %s: a probe endpoint holds no session, got %+v", who, e.Endpoint, e.Session)
			}
			w, gated := want[e.Endpoint]
			if !gated {
				if e.Rejection != nil {
					t.Errorf("%s %s: a lifted gate serves nothing, got %+v", who, e.Endpoint, e.Rejection)
				}
				continue
			}
			if e.Rejection == nil {
				t.Errorf("%s %s: no gate served beside no session", who, e.Endpoint)
				continue
			}
			if e.Rejection.Rung != w.rung || e.Rejection.Class != w.class || e.Rejection.Reason != w.reason {
				t.Errorf("%s %s: gate = %s %s %q, want %s %s %q", who, e.Endpoint, e.Rejection.Rung, e.Rejection.Class, e.Rejection.Reason, w.rung, w.class, w.reason)
			}
			if !e.Rejection.TS.Equal(at) {
				t.Errorf("%s %s: gate ts = %v, want its publish time %v", who, e.Endpoint, e.Rejection.TS, at)
			}
		}
		if len(ids) != 5 {
			t.Fatalf("%s: endpoints = %v, want five", who, ids)
		}
		return ids
	}
	ids := check("owner", read(ownerTok))

	// A viewer reads the same refusal (component:read), and its retry is
	// refused: endpoint:retry is an operator's, so the console greys Retry
	// and the server holds the line anyway.
	viewerTok := setupAllViewer(t, ctx, dsn, "viewer-all")
	check("viewer", read(viewerTok))
	for name := range want {
		c.do(viewerTok, http.MethodPost, "/endpoints/"+ids[name]+":retry", nil, http.StatusForbidden)
		c.do(ownerTok, http.MethodPost, "/endpoints/"+ids[name]+":retry", nil, http.StatusOK)
	}
}
