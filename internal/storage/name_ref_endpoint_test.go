package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// endpointNameFixture is two rooms under one campus, each holding a component
// named display-1 (names are unique only within a placement), both carrying an
// https endpoint, and room-a's alone carrying an ssh one.
type endpointNameFixture struct {
	gw             storage.Gateway
	dsn            string
	dispA, dispB   *storage.Component
	httpsA, httpsB *storage.Endpoint
	sshA           *storage.Endpoint
	scopeA, scopeB scope.Set
	addrA, addrB   string
}

func newEndpointNameFixture(t *testing.T) *endpointNameFixture {
	t.Helper()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mustLoc(t, gw, "campus", "campus", nil)
	mustLoc(t, gw, "room-a", "room", strptr("campus"))
	mustLoc(t, gw, "room-b", "room", strptr("campus"))
	f := &endpointNameFixture{gw: gw, dsn: dsn}
	f.dispA = mustCreateComponent(t, gw, storage.ComponentSpec{Name: "display-1", LocationName: strptr("room-a")}, all)
	f.dispB = mustCreateComponent(t, gw, storage.ComponentSpec{Name: "display-1", LocationName: strptr("room-b")}, all)
	f.addrA, f.addrB = "campus.room-a.$comp.display-1", "campus.room-b.$comp.display-1"
	mk := func(transport, comp string) *storage.Endpoint {
		t.Helper()
		ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: transport, Component: strptr(comp)}, all)
		if err != nil {
			t.Fatalf("create %s on %s: %v", transport, comp, err)
		}
		return ep
	}
	f.httpsA, f.httpsB, f.sshA = mk("https", f.addrA), mk("https", f.addrB), mk("ssh", f.addrA)
	f.scopeA, f.scopeB = scope.Set{IDs: []string{f.dispA.ID}}, scope.Set{IDs: []string{f.dispB.ID}}
	return f
}

// TestEndpointAnswersToItsAddress pins #1133's endpoint key: an endpoint is
// `<component>/<endpoint name>`, the component spelled any way the component
// resolver takes it (a dotted address, a bare name, a uuid), and its own uuid
// keeps working. Whatever form found it, the endpoint returned carries its uuid.
func TestEndpointAnswersToItsAddress(t *testing.T) {
	f := newEndpointNameFixture(t)
	ctx := context.Background()

	for _, ref := range []string{
		f.addrA + "/https",    // the dotted address
		f.dispA.ID + "/https", // the component by uuid
		f.httpsA.ID,           // the endpoint's own uuid
	} {
		got, err := f.gw.GetEndpoint(ctx, ref, all)
		if err != nil {
			t.Fatalf("get %q: %v", ref, err)
		}
		if got.ID != f.httpsA.ID {
			t.Errorf("get %q = endpoint %s, want room-a's https %s", ref, got.ID, f.httpsA.ID)
		}
	}

	// A bare component name that two components share still resolves when only
	// one of them carries the endpoint asked for: the relation narrows the
	// candidates before ambiguity is judged.
	got, err := f.gw.GetEndpoint(ctx, "display-1/ssh", all)
	if err != nil || got.ID != f.sshA.ID {
		t.Fatalf("get display-1/ssh = %+v (err %v), want room-a's ssh", got, err)
	}
}

// TestEndpointNameAmbiguityIsJudgedInScope pins the ambiguity rule: a name two
// endpoints the caller can see both answer to is refused with each one's full
// address, and a caller who can see only one of them resolves it cleanly, never
// learning the other exists.
func TestEndpointNameAmbiguityIsJudgedInScope(t *testing.T) {
	f := newEndpointNameFixture(t)
	ctx := context.Background()

	_, err := f.gw.GetEndpoint(ctx, "display-1/https", all)
	var unresolved *storage.ErrUnresolvedRef
	if !errors.As(err, &unresolved) {
		t.Fatalf("all-scope get display-1/https = %v, want ErrUnresolvedRef", err)
	}
	if !errors.Is(err, storage.ErrEndpointNotFound) {
		t.Errorf("an unresolved endpoint ref must still be ErrEndpointNotFound for the callers that fold it")
	}
	want := []string{f.addrA + "/https", f.addrB + "/https"}
	if strings.Join(unresolved.Candidates, ",") != strings.Join(want, ",") {
		t.Errorf("candidates = %v, want %v", unresolved.Candidates, want)
	}
	if unresolved.Kind != "endpoint" || unresolved.Ref != "display-1/https" {
		t.Errorf("unresolved = %+v, want kind endpoint naming the ref as sent", unresolved)
	}

	// Scoped to room-a's display, the same name names one endpoint.
	got, err := f.gw.GetEndpoint(ctx, "display-1/https", f.scopeA)
	if err != nil || got.ID != f.httpsA.ID {
		t.Fatalf("room-a-scoped get display-1/https = %+v (err %v), want room-a's https", got, err)
	}
}

// TestEndpointNameLookupDisclosesNothingOutOfScope pins the non-disclosure: an
// endpoint that exists outside the caller's read scope and one that does not
// exist at all give the same refusal, in every spelling, with no candidate.
func TestEndpointNameLookupDisclosesNothingOutOfScope(t *testing.T) {
	f := newEndpointNameFixture(t)
	ctx := context.Background()

	refusal := func(ref string) string {
		t.Helper()
		_, err := f.gw.GetEndpoint(ctx, ref, f.scopeA)
		var unresolved *storage.ErrUnresolvedRef
		if !errors.As(err, &unresolved) {
			t.Fatalf("room-a-scoped get %q = %v, want ErrUnresolvedRef", ref, err)
		}
		if len(unresolved.Candidates) != 0 {
			t.Errorf("room-a-scoped get %q named candidates %v", ref, unresolved.Candidates)
		}
		return unresolved.Kind + "|" + strings.ReplaceAll(unresolved.Error(), ref, "REF")
	}
	hidden := refusal(f.addrB + "/https")
	absent := refusal("campus.room-b.$comp.display-9/https")
	if hidden != absent {
		t.Errorf("out-of-scope refusal %q differs from absent refusal %q", hidden, absent)
	}
	if refusal(f.dispB.ID+"/https") != absent {
		t.Errorf("an out-of-scope component uuid refuses differently from an absent one")
	}
	if refusal(f.addrA+"/telnet") != absent {
		t.Errorf("an absent endpoint name on a readable component refuses differently")
	}
	// The endpoint's own uuid, out of scope, is the plain non-disclosing miss.
	if _, err := f.gw.GetEndpoint(ctx, f.httpsB.ID, f.scopeA); !errors.Is(err, storage.ErrEndpointNotFound) {
		t.Errorf("out-of-scope uuid = %v, want ErrEndpointNotFound", err)
	}
}

// TestEndpointRefMalformed: a reference that is neither a uuid nor a
// component/endpoint pair names nothing, and says how an endpoint is addressed.
func TestEndpointRefMalformed(t *testing.T) {
	f := newEndpointNameFixture(t)
	ctx := context.Background()
	for _, ref := range []string{"https", "display-1/https/extra", "/https", "display-1/"} {
		_, err := f.gw.GetEndpoint(ctx, ref, all)
		var unresolved *storage.ErrUnresolvedRef
		if !errors.As(err, &unresolved) {
			t.Fatalf("get %q = %v, want ErrUnresolvedRef", ref, err)
		}
		if !strings.Contains(unresolved.Hint, "<component>/<endpoint>") {
			t.Errorf("get %q hint = %q, want it to say how an endpoint is addressed", ref, unresolved.Hint)
		}
	}
}

// TestEndpointWritesByNameKeepTheScopeSplit pins that a name changes how the
// target is found and nothing about what may be done to it: update, retry and
// delete by address run the same read-then-action split as by uuid (out of
// read scope is the non-disclosing miss, readable but out of action scope the
// 403), and the audit row keys on the endpoint's uuid, never the spelling.
func TestEndpointWritesByNameKeepTheScopeSplit(t *testing.T) {
	f := newEndpointNameFixture(t)
	ctx := context.Background()
	refA, refB := f.addrA+"/https", f.addrB+"/https"
	label := "Wall display"

	if _, err := f.gw.UpdateEndpoint(ctx, "", refB, storage.EndpointPatch{Label: &label}, f.scopeA, f.scopeA); !errors.Is(err, storage.ErrEndpointNotFound) {
		t.Errorf("update out-of-read-scope by name = %v, want ErrEndpointNotFound", err)
	}
	if _, err := f.gw.UpdateEndpoint(ctx, "", refA, storage.EndpointPatch{Label: &label}, f.scopeA, scope.Set{}); !errors.Is(err, storage.ErrEndpointForbidden) {
		t.Errorf("update readable-not-actionable by name = %v, want ErrEndpointForbidden", err)
	}
	upd, err := f.gw.UpdateEndpoint(ctx, "", refA, storage.EndpointPatch{Label: &label}, f.scopeA, f.scopeA)
	if err != nil || upd.ID != f.httpsA.ID || upd.Label != label {
		t.Fatalf("update by name = %+v (err %v), want room-a's https relabelled", upd, err)
	}

	if _, err := f.gw.RetryEndpoint(ctx, "", refA, storage.RetryPin{}, f.scopeA, scope.Set{}); !errors.Is(err, storage.ErrEndpointForbidden) {
		t.Errorf("retry readable-not-actionable by name = %v, want ErrEndpointForbidden", err)
	}
	if _, err := f.gw.RetryEndpoint(ctx, "", refB, storage.RetryPin{}, f.scopeA, f.scopeA); !errors.Is(err, storage.ErrEndpointNotFound) {
		t.Errorf("retry out-of-read-scope by name = %v, want ErrEndpointNotFound", err)
	}
	retried, err := f.gw.RetryEndpoint(ctx, "", refA, storage.RetryPin{}, f.scopeA, f.scopeA)
	if err != nil || retried.ID != f.httpsA.ID {
		t.Fatalf("retry by name = %+v (err %v)", retried, err)
	}

	if err := f.gw.DeleteEndpoint(ctx, "", refB, f.scopeA, f.scopeA); !errors.Is(err, storage.ErrEndpointNotFound) {
		t.Errorf("delete out-of-read-scope by name = %v, want ErrEndpointNotFound", err)
	}
	if err := f.gw.DeleteEndpoint(ctx, "", refA, f.scopeA, f.scopeA); err != nil {
		t.Fatalf("delete by name: %v", err)
	}

	conn, err := pgx.Connect(ctx, f.dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var keyed, spelled int
	if err := conn.QueryRow(ctx, `select count(*) filter (where resource_id = $1), count(*) filter (where resource_id like '%/%')
		from audit_log where resource = 'endpoint' and verb in ('update', 'retry', 'delete')`, f.httpsA.ID).Scan(&keyed, &spelled); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if keyed != 3 || spelled != 0 {
		t.Errorf("audit rows keyed on the uuid = %d, on a spelling = %d; want 3 and 0", keyed, spelled)
	}
}
