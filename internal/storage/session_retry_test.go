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

// The session's auth layer on the server side (#1049): the worklist carries
// the driver's session block and the endpoint's revision, and an operator's
// :retry moves the revision (which is what wakes a rejected session on the
// node) and is audited. Skipped under -short.

// loginLineSpec is a line protocol behind a password prompt: a login dialog
// over tcp, its failure line terminal.
const loginLineSpec = `{
	"version": 1,
	"transport": "tcp",
	"inputs": [
		{"name": "host", "kind": "string", "required": true},
		{"name": "port", "kind": "number", "default": "23"},
		{"name": "login", "kind": "secret", "secret_type": "basic-auth", "required": true}
	],
	"session": {
		"login": {
			"steps": [
				{"expect": {"regex": "login: ?$"}, "send": "${secret.login.username}"},
				{"expect": {"regex": "Password: ?$"}, "send": "${secret.login.password}"}
			],
			"ready": {"prefix": "READY"}
		},
		"terminal": [{"class": "auth", "match": {"prefix": "auth rejected"}}],
		"reprobe": "1h"
	},
	"polls": [{
		"name": "status",
		"schedule": {"every": "30s"},
		"request": {"line": "GET INPUT", "reply": {"prefix": "INPUT "}},
		"emits": [{"name": "video-input", "extract": {"regex": "^INPUT (\\S+)$"}}]
	}],
	"commands": [
		{"command_type": "set-input", "request": {"line": "SET INPUT ${value}", "reply": {"prefix": "INPUT "}}}
	]
}`

func TestWorklistCarriesTheSessionBlockAndRetryMovesTheRevision(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-1"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "panel-login", SecretType: "basic-auth", OwnerKind: "platform",
		Fields: map[string]string{"username": "admin", "password": "newtron"},
	}, all, true); err != nil {
		t.Fatalf("create secret: %v", err)
	}
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "panel-login", Label: "Panel Login", Version: "1.0.0", Spec: []byte(loginLineSpec)}); err != nil {
		t.Fatalf("create driver: %v", err)
	}
	comp, drv, node := "panel-1", "panel-login", "edge-1"
	ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &drv, Component: &comp, Node: &node,
		Inputs:     map[string]string{"host": "10.20.4.41", "login": "panel-login"},
		SecretRead: all, CanAdmin: true,
	}, all)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}

	driverTask := func() storage.WorklistTask {
		t.Helper()
		wl, err := gw.NodeWorklist(ctx, "edge-1")
		if err != nil {
			t.Fatalf("worklist: %v", err)
		}
		for _, task := range wl.Tasks {
			if len(task.Spec) > 2 {
				return task
			}
		}
		t.Fatalf("no driver task in %+v", wl.Tasks)
		return storage.WorklistTask{}
	}
	first := driverTask()
	if first.Session == nil || first.Session.Login == nil || len(first.Session.Login.Steps) != 2 || first.Session.Reprobe != "1h" {
		t.Fatalf("the worklist session block = %+v, want the driver's login and re-probe", first.Session)
	}
	if first.Secrets["login"]["password"] != "newtron" {
		t.Fatalf("the login's credential was not delivered: %v", first.Secrets)
	}
	if first.SessionRetry != 0 {
		t.Fatalf("a fresh endpoint's retry count = %d, want 0", first.SessionRetry)
	}
	// An unrelated edit (a label) moves nothing a rejected session reads: it
	// must not pull the session out of rejected (#1056 ruling).
	label := "Lobby panel"
	if _, err := gw.UpdateEndpoint(ctx, "", ep.ID, storage.EndpointPatch{Label: &label}, all, all); err != nil {
		t.Fatalf("label edit: %v", err)
	}
	if got := driverTask().SessionRetry; got != 0 {
		t.Fatalf("a label edit moved the retry count to %d", got)
	}

	// A viewer-shaped caller (no action scope) cannot retry; out of read
	// scope it is not even told the endpoint exists.
	none := scope.Set{}
	if _, err := gw.RetryEndpoint(ctx, "", ep.ID, "", all, none); !errors.Is(err, storage.ErrEndpointForbidden) {
		t.Fatalf("retry without action scope = %v, want forbidden", err)
	}
	if _, err := gw.RetryEndpoint(ctx, "", ep.ID, "", none, none); !errors.Is(err, storage.ErrEndpointNotFound) {
		t.Fatalf("retry out of read scope = %v, want not found", err)
	}

	if _, err := gw.RetryEndpoint(ctx, "", ep.ID, "", all, all); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := driverTask().SessionRetry; got != 1 {
		t.Fatalf("retry count after one :retry = %d, want 1", got)
	}
	// Every task of the endpoint (its poll and its reachability probe) is
	// judged against one fingerprint, which the retry moved.
	wl, err := gw.NodeWorklist(ctx, "edge-1")
	if err != nil {
		t.Fatal(err)
	}
	fps := map[string]bool{}
	for _, task := range wl.Tasks {
		fps[task.Fingerprint] = true
	}
	if len(fps) != 1 || fps[first.Fingerprint] || fps[""] {
		t.Fatalf("the endpoint's tasks carry fingerprints %v, want one, moved by the retry", fps)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var n int
	if err := conn.QueryRow(ctx, `select count(*) from audit_log where resource = 'endpoint' and verb = 'retry' and resource_id = $1`, ep.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("audit rows for the retry = %d, want 1", n)
	}

	// A session-only driver's command delivery says so: the node rides the
	// held session or fails, never a throwaway connection that would skip
	// the login.
	_, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	var actor string
	if err := conn.QueryRow(ctx, `select principal_id from human where username = 'root'`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.IssueCommand(ctx, actor, "component", "panel-1", "set-input", "", json.RawMessage(`"hdmi2"`), nil); err != nil {
		t.Fatalf("issue: %v", err)
	}
	got, err := gw.PendingNodeCommands(ctx, "edge-1")
	if err != nil || len(got) != 1 {
		t.Fatalf("pending = %+v, %v", got, err)
	}
	if !got[0].SessionOnly {
		t.Fatalf("delivery = %+v, want session-only for a driver that declares a login", got[0])
	}
}

// Review of the fingerprint slice, finding 2: a host key the node trusted on
// first use (the observed ssh-host-key) is a learned fact, not an input the
// operator set, so it must not move the fingerprint (that would recycle a
// healthy session once, and lift a gate held under the first fingerprint).
// The operator's declared pin does.
func TestATrustedOnFirstUseKeyLeavesTheFingerprint(t *testing.T) {
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
	comp, node := "codec-1", "edge-k"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: comp}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: node}, all, all); err != nil {
		t.Fatal(err)
	}
	ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "ssh", Component: &comp, Node: &node,
		Params: []byte(`{"target":"10.0.0.9:22"}`)}, all)
	if err != nil {
		t.Fatal(err)
	}
	fp := func() (string, string) {
		t.Helper()
		wl, err := gw.NodeWorklist(ctx, node)
		if err != nil || len(wl.Tasks) == 0 {
			t.Fatalf("worklist: %v %+v", err, wl)
		}
		return wl.Tasks[0].Fingerprint, wl.Tasks[0].HostKey
	}
	before, _ := fp()
	key := "SHA256:" + strings.Repeat("B", 43)
	if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: comp, Key: "ssh-host-key",
		Instance: ep.Name, Value: key, Source: "ssh", TS: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	after, pin := fp()
	if pin != key {
		t.Fatalf("the worklist pin = %q, want the trusted key", pin)
	}
	if after != before {
		t.Fatal("a key trusted on first use moved the fingerprint")
	}
}

// Trusting a changed key from a refusal (#1078): the key a :retry pins while
// the endpoint's gate names an offered key must be that key, so a confirm the
// operator answered against a key the component has since stopped offering
// (it changed again, or something else now answers) is refused and changes
// nothing; the accepted trust is audited with the pin it replaced, the pin it
// set, and the actor. With no key refusal held, a host key is a pin declared
// ahead of time, as before.
func TestTrustingAKeyChecksTheOfferedKeyAndAuditsBoth(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	_, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var actor string
	if err := conn.QueryRow(ctx, `select principal_id from human where username = 'root'`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	comp, node := "codec-2", "edge-t"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: comp}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: node}, all, all); err != nil {
		t.Fatal(err)
	}
	ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "ssh", Component: &comp, Node: &node,
		Params: []byte(`{"target":"10.0.0.10:22"}`)}, all)
	if err != nil {
		t.Fatal(err)
	}
	first, shown, now := "SHA256:"+strings.Repeat("F", 43), "SHA256:"+strings.Repeat("S", 43), "SHA256:"+strings.Repeat("N", 43)
	observe := func(key, value string) {
		t.Helper()
		if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: comp, Key: key,
			Instance: ep.Name, Value: value, Source: "ssh", TS: time.Now()}}); err != nil {
			t.Fatal(err)
		}
	}
	task := func() storage.WorklistTask {
		t.Helper()
		wl, err := gw.NodeWorklist(ctx, node)
		if err != nil || len(wl.Tasks) == 0 {
			t.Fatalf("worklist: %v %+v", err, wl)
		}
		return wl.Tasks[0]
	}
	audits := func() int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, `select count(*) from audit_log where resource = 'endpoint' and verb = 'retry' and resource_id = $1`, ep.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// The node trusted the first key, then the component offered another: the
	// console showed `shown`. Before the operator confirmed, the component
	// changed again and the node published the gate naming `now`.
	observe("ssh-host-key", first)
	observe("endpoint-rejection", "responsive host-key "+task().Fingerprint+" "+first+" "+now)
	before := task()
	if _, err := gw.RetryEndpoint(ctx, actor, ep.ID, shown, all, all); !errors.Is(err, storage.ErrRetryHostKeyStale) {
		t.Fatalf("trusting a key the component no longer offers = %v, want stale", err)
	} else if !strings.Contains(err.Error(), now) || !strings.Contains(err.Error(), "on record") || !strings.Contains(err.Error(), "retry the endpoint without a host key") {
		// The gate is the node's last word: a gated node dials no more, so
		// the component may have moved on since (review of #1078). The
		// refusal says what the record names and how to read the key again,
		// never that the component offers it now.
		t.Fatalf("the stale refusal does not name the key on record and the way to read it again: %v", err)
	}
	if after := task(); after.HostKey != first || after.SessionRetry != before.SessionRetry || after.Fingerprint != before.Fingerprint {
		t.Fatalf("a refused trust changed the endpoint: pin %q, retry %d", after.HostKey, after.SessionRetry)
	}
	if n := audits(); n != 0 {
		t.Fatalf("a refused trust wrote %d audit rows", n)
	}

	// The key the gate names is trusted: it becomes the pin, the retry
	// counter moves, and the audit row carries both keys and the actor.
	if _, err := gw.RetryEndpoint(ctx, actor, ep.ID, now, all, all); err != nil {
		t.Fatalf("trusting the offered key: %v", err)
	}
	if after := task(); after.HostKey != now || after.SessionRetry != before.SessionRetry+1 {
		t.Fatalf("after trusting: pin %q, retry %d", after.HostKey, after.SessionRetry)
	}
	var oldKey, newKey, actorID string
	if err := conn.QueryRow(ctx, `select coalesce(old->>'host_key', ''), coalesce(new->>'host_key', ''), coalesce(actor_principal_id::text, '')
		from audit_log where resource = 'endpoint' and verb = 'retry' and resource_id = $1`, ep.ID).Scan(&oldKey, &newKey, &actorID); err != nil {
		t.Fatal(err)
	}
	if oldKey != first || newKey != now || actorID != actor {
		t.Fatalf("audit: old %q new %q actor %q, want %q -> %q by %q", oldKey, newKey, actorID, first, now, actor)
	}

	// With no key refusal held, a key is a pin declared ahead of time.
	observe("endpoint-rejection", "none")
	if _, err := gw.RetryEndpoint(ctx, actor, ep.ID, shown, all, all); err != nil {
		t.Fatalf("declaring a pin with no key refusal held: %v", err)
	}
	if got := task().HostKey; got != shown {
		t.Fatalf("pin = %q, want the declared %q", got, shown)
	}
}
