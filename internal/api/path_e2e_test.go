package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// pathTestServer boots a real Postgres-backed server with an owner principal,
// the shared harness every dotted-address e2e in this file drives requests
// against. Skipped under -short (storagetest.NewDSN handles that).
func pathTestServer(t *testing.T) (*apiClient, string) {
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

	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	t.Cleanup(srv.Close)
	return &apiClient{t: t, ctx: ctx, base: srv.URL}, ownerTok
}

// TestGetComponentAlarmsByAmbiguousNameIs409 is the task brief's own required
// regression (Step 4): before mapAlarmErr delegated to the shared mapRefErr
// (#627 Task 11), it sat on its own `default: 500` branch, so a duplicate-name
// GET on a component-scoped nested route (not just the bare GET /components/
// {name} Task 11's own e2e already covers) turned into an unmapped internal
// error the moment two rooms held the same name. Proves the wiring reaches a
// SECOND route through the same mapper, not just the one every prior task
// happened to test.
func TestGetComponentAlarmsByAmbiguousNameIs409(t *testing.T) {
	c, tok := pathTestServer(t)

	c.do(tok, http.MethodPost, "/components", map[string]any{"name": "dup-alarm", "product": "generic-device"}, http.StatusCreated)
	c.do(tok, http.MethodPost, "/components", map[string]any{"name": "holder-alarm", "product": "generic-device"}, http.StatusCreated)
	c.do(tok, http.MethodPost, "/components", map[string]any{"name": "dup-alarm", "product": "generic-device", "parent": "holder-alarm"}, http.StatusCreated)

	status, body := c.send(tok, http.MethodGet, "/components/dup-alarm/alarms", nil)
	if status != http.StatusConflict {
		t.Fatalf("GET .../alarms on an ambiguous component status = %d, want 409\nbody: %s", status, body)
	}
	if !strings.Contains(string(body), "dup-alarm") {
		t.Fatalf("409 body = %s, want it to name the ambiguous reference", body)
	}
}

// TestGetComponentByDottedAddressEndToEnd drives a real dotted address
// through the full stack: HTTP -> Huma path binding -> ParseAddress ->
// resolvePath -> the ordinary scope-checked read. The task brief's own
// example address, verbatim.
func TestGetComponentByDottedAddressEndToEnd(t *testing.T) {
	c, tok := pathTestServer(t)

	c.do(tok, http.MethodPost, "/locations", map[string]any{"name": "boi", "location_type": "campus"}, http.StatusCreated)
	c.do(tok, http.MethodPost, "/locations", map[string]any{"name": "17c", "location_type": "building", "parent": "boi"}, http.StatusCreated)
	c.do(tok, http.MethodPost, "/locations", map[string]any{"name": "415a", "location_type": "room", "parent": "17c"}, http.StatusCreated)
	c.do(tok, http.MethodPost, "/components", map[string]any{"name": "display-1", "product": "generic-device", "location": "415a"}, http.StatusCreated)

	body := c.do(tok, http.MethodGet, "/components/boi.17c.415a.$comp.display-1", nil, http.StatusOK)
	if !strings.Contains(string(body), `"name":"display-1"`) {
		t.Fatalf("GET by dotted address body = %s, want the display-1 component", body)
	}

	// A structural miss (a segment that does not exist) is the component's
	// own non-disclosing 404, not a 500 and not an internal error leaking the
	// walk's shape.
	status, missBody := c.send(tok, http.MethodGet, "/components/boi.17c.999z.$comp.display-1", nil)
	if status != http.StatusNotFound {
		t.Fatalf("GET by an address with a missing segment status = %d, want 404\nbody: %s", status, missBody)
	}
}

// TestGetSystemByDottedAddressEndToEnd covers the $sys accessor over HTTP.
func TestGetSystemByDottedAddressEndToEnd(t *testing.T) {
	c, tok := pathTestServer(t)

	c.do(tok, http.MethodPost, "/locations", map[string]any{"name": "boi", "location_type": "campus"}, http.StatusCreated)
	c.do(tok, http.MethodPost, "/locations", map[string]any{"name": "17c", "location_type": "building", "parent": "boi"}, http.StatusCreated)
	c.do(tok, http.MethodPost, "/systems", map[string]any{"name": "av", "location": "17c"}, http.StatusCreated)

	body := c.do(tok, http.MethodGet, "/systems/boi.17c.$sys.av", nil, http.StatusOK)
	if !strings.Contains(string(body), `"name":"av"`) {
		t.Fatalf("GET by dotted system address body = %s, want the av system", body)
	}
}

// TestGetComponentByEmptySegmentIs422 drives the empty-segment case (the
// shell-quoting failure the parser's message names) through the real HTTP
// path, not just the pure parser unit table: an unquoted $comp in a shell
// leaves a literal empty segment between two dots by the time it reaches the
// handler.
func TestGetComponentByEmptySegmentIs422(t *testing.T) {
	c, tok := pathTestServer(t)

	status, body := c.send(tok, http.MethodGet, "/components/boi.17c.415a..display-1", nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("GET with an empty address segment status = %d, want 422\nbody: %s", status, body)
	}
	if !strings.Contains(string(body), "must be quoted in a shell") {
		t.Fatalf("422 body = %s, want it to name the shell cause", body)
	}
}

// TestGetComponentByLocationAddressIs404 proves a kind-mismatched address (a
// pure location path, no $comp accessor, asked of the component resolver) is
// the same non-disclosing 404 a missing component gets, not a 500 and not a
// disclosure that the address resolved to a real row of a different kind.
func TestGetComponentByLocationAddressIs404(t *testing.T) {
	c, tok := pathTestServer(t)

	c.do(tok, http.MethodPost, "/locations", map[string]any{"name": "boi", "location_type": "campus"}, http.StatusCreated)
	c.do(tok, http.MethodPost, "/locations", map[string]any{"name": "17c", "location_type": "building", "parent": "boi"}, http.StatusCreated)

	status, body := c.send(tok, http.MethodGet, "/components/boi.17c", nil)
	if status != http.StatusNotFound {
		t.Fatalf("GET a component by a location address status = %d, want 404\nbody: %s", status, body)
	}
}

// TestGetNodeByDottedRefIs422 is the registry-side companion: a node's name
// stays a single global token (#627 Task 12), so a dotted ref must be an
// explicit 422 naming that, never a fall-through 404 that looks identical to
// an ordinary unknown node.
func TestGetNodeByDottedRefIs422(t *testing.T) {
	c, tok := pathTestServer(t)

	status, body := c.send(tok, http.MethodGet, "/nodes/boi.17c", nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("GET a node by a dotted ref status = %d, want 422\nbody: %s", status, body)
	}
	if !strings.Contains(string(body), "single token") {
		t.Fatalf("422 body = %s, want it to say a node name is a single token", body)
	}
}

// TestGetMetricTypeByDottedRefIs422 is the lane-type companion: metric_type
// (and its three siblings, wired identically) stays a single global token.
func TestGetMetricTypeByDottedRefIs422(t *testing.T) {
	c, tok := pathTestServer(t)

	status, body := c.send(tok, http.MethodGet, "/metric-types/icmp.rtt-avg", nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("GET a metric type by a dotted ref status = %d, want 422\nbody: %s", status, body)
	}
	if !strings.Contains(string(body), "single token") {
		t.Fatalf("422 body = %s, want it to say a metric_type name is a single token", body)
	}
}

// Fix round 1 (task-12-review.md findings 1 and 2): a dotted reference
// carried in a request BODY (a create's location or parent, none of which
// carry a pattern tag) must produce the same status the bare-name form
// already does, not the entity's own non-disclosing 404 that mapRefErr's
// unconditional ErrPathNotFound case would otherwise give it.

// TestCreateComponentWithDottedMissingLocationIs422 is the review's own
// scenario, driven over real HTTP: POST /components with a dotted "location"
// that structurally misses.
func TestCreateComponentWithDottedMissingLocationIs422(t *testing.T) {
	c, tok := pathTestServer(t)

	c.do(tok, http.MethodPost, "/locations", map[string]any{"name": "boi", "location_type": "campus"}, http.StatusCreated)

	status, body := c.send(tok, http.MethodPost, "/components",
		map[string]any{"name": "x", "product": "generic-device", "location": "boi.nope"})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("POST /components with a dotted-missing location status = %d, want 422\nbody: %s", status, body)
	}
	if !strings.Contains(string(body), "location") {
		t.Fatalf("422 body = %s, want it to name the location reference", body)
	}
}

// TestCreateComponentWithDottedMissingParentIs422 is the "worse than merely
// inconsistent" variant the review names: a dotted-but-missing parent must
// still be reported as a parent problem (422), not a 404 naming the
// component being CREATED, which cannot exist yet.
func TestCreateComponentWithDottedMissingParentIs422(t *testing.T) {
	c, tok := pathTestServer(t)

	status, body := c.send(tok, http.MethodPost, "/components",
		map[string]any{"name": "x", "product": "generic-device", "parent": "$comp.nope"})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("POST /components with a dotted-missing parent status = %d, want 422\nbody: %s", status, body)
	}
	if !strings.Contains(string(body), "parent") {
		t.Fatalf("422 body = %s, want it to name the parent reference, not the component being created", body)
	}
}
