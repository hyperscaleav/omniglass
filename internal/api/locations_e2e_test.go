package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestLocationAPI drives the location surface over HTTP as the user would: an
// owner builds a tree and runs full CRUD, and a location-scoped viewer sees only
// its subtree, gets a non-disclosing 404 outside it, and is forbidden a write.
// The fine-grained scope-403 backstop (readable but outside the action scope) is
// proven at the gateway in TestLocationScopeCRUD; here the viewer's write 403 is
// the capability fast-reject. Skipped under -short.
func TestLocationAPI(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
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

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Owner builds the tree: hq (campus) > hq-b1 (building) > hq-r1 (room); lab.
	c.create(ownerTok, locReq{Name: "hq", LocationType: "campus"}, http.StatusCreated)
	c.create(ownerTok, locReq{Name: "hq-b1", LocationType: "building", Parent: ptr("hq")}, http.StatusCreated)
	c.create(ownerTok, locReq{Name: "hq-r1", LocationType: "room", Parent: ptr("hq-b1")}, http.StatusCreated)
	c.create(ownerTok, locReq{Name: "lab", LocationType: "campus"}, http.StatusCreated)

	// A root creation by the owner works; an unknown type is a 422.
	c.create(ownerTok, locReq{Name: "bad", LocationType: "galaxy"}, http.StatusUnprocessableEntity)

	// Owner sees all four.
	if got := c.list(ownerTok); len(got) != 4 {
		t.Fatalf("owner list = %d, want 4", len(got))
	}

	// A viewer scoped to hq: reads only the hq subtree.
	hqID := c.list(ownerTok)[mustIndex(t, c.list(ownerTok), "hq")].ID
	viewerTok := setupScopedViewer(t, ctx, dsn, "viewer-hq", "viewer", "location", hqID)

	got := c.list(viewerTok)
	if len(got) != 3 {
		t.Fatalf("viewer-hq list = %d, want 3 (hq subtree)", len(got))
	}
	for _, l := range got {
		if l.Name == "lab" {
			t.Fatal("viewer-hq leaked lab")
		}
	}

	// Non-disclosing 404 for a location outside the read scope; 200 inside it.
	c.get(viewerTok, "lab", http.StatusNotFound)
	c.get(viewerTok, "hq-b1", http.StatusOK)

	// The viewer cannot write: PATCH is a 403 at the capability fast-reject.
	c.patch(viewerTok, "hq-b1", patchReq{DisplayName: ptr("nope")}, http.StatusForbidden)

	// Owner full CRUD: patch, then delete-occupied 409, then leaf delete, then 404.
	c.patch(ownerTok, "hq-b1", patchReq{DisplayName: ptr("Building One")}, http.StatusOK)
	if l := c.getBody(ownerTok, "hq-b1"); l.DisplayName != "Building One" {
		t.Errorf("patched display_name = %q, want Building One", l.DisplayName)
	}
	c.delete(ownerTok, "hq", http.StatusConflict) // has children
	c.delete(ownerTok, "hq-r1", http.StatusNoContent)
	c.get(ownerTok, "hq-r1", http.StatusNotFound)
}

// --- tiny HTTP client over the location surface -----------------------------

type locReq struct {
	Name         string  `json:"name"`
	DisplayName  string  `json:"display_name,omitempty"`
	LocationType string  `json:"location_type"`
	Parent       *string `json:"parent,omitempty"`
}

type patchReq struct {
	DisplayName  *string `json:"display_name,omitempty"`
	LocationType *string `json:"location_type,omitempty"`
}

type locResp struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`
	LocationType string `json:"location_type"`
}

type apiClient struct {
	t    *testing.T
	ctx  context.Context
	base string
}

// send issues a request and returns the status and body without asserting, for
// callers that test the status itself (the route-gating guard).
func (c *apiClient) send(tok, method, path string, body any) (int, []byte) {
	c.t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(c.ctx, method, c.base+"/api/v1"+path, rdr)
	if err != nil {
		c.t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("send %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func (c *apiClient) do(tok, method, path string, body any, want int) []byte {
	c.t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(c.ctx, method, c.base+"/api/v1"+path, rdr)
	if err != nil {
		c.t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("do %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("read body %s %s: %v", method, path, err)
	}
	if resp.StatusCode != want {
		c.t.Fatalf("%s %s = %d, want %d (body %s)", method, path, resp.StatusCode, want, out)
	}
	return out
}

func (c *apiClient) create(tok string, r locReq, want int) {
	c.do(tok, http.MethodPost, "/locations", r, want)
}
func (c *apiClient) patch(tok, name string, r patchReq, want int) {
	c.do(tok, http.MethodPatch, "/locations/"+name, r, want)
}
func (c *apiClient) get(tok, name string, want int) {
	c.do(tok, http.MethodGet, "/locations/"+name, nil, want)
}
func (c *apiClient) delete(tok, name string, want int) {
	c.do(tok, http.MethodDelete, "/locations/"+name, nil, want)
}

func (c *apiClient) getBody(tok, name string) locResp {
	c.t.Helper()
	out := c.do(tok, http.MethodGet, "/locations/"+name, nil, http.StatusOK)
	var l locResp
	if err := json.Unmarshal(out, &l); err != nil {
		c.t.Fatalf("decode location: %v", err)
	}
	return l
}

func (c *apiClient) list(tok string) []locResp {
	c.t.Helper()
	out := c.do(tok, http.MethodGet, "/locations", nil, http.StatusOK)
	var body struct {
		Locations []locResp `json:"locations"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		c.t.Fatalf("decode list: %v", err)
	}
	return body.Locations
}

func ptr(s string) *string { return &s }

func mustIndex(t *testing.T, locs []locResp, name string) int {
	t.Helper()
	for i, l := range locs {
		if l.Name == name {
			return i
		}
	}
	t.Fatalf("location %q not in list", name)
	return -1
}

// setupScopedViewer inserts a service principal with a single (role @ scope)
// grant and returns its bearer token, so the test can drive a scoped identity.
func setupScopedViewer(t *testing.T, ctx context.Context, dsn, label, role, scopeKind, scopeID string) string {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var pid string
	if err := conn.QueryRow(ctx, `insert into principal (kind) values ('service') returning id`).Scan(&pid); err != nil {
		t.Fatalf("insert principal: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into service (principal_id, label) values ($1, $2)`, pid, label); err != nil {
		t.Fatalf("insert service: %v", err)
	}
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`insert into credential (principal_id, kind, secret_hash, prefix) values ($1, 'bearer', $2, $3)`,
		pid, hash, prefix); err != nil {
		t.Fatalf("insert credential: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`insert into principal_grant (principal_id, role_id, scope_kind, scope_id) values ($1, $2, $3, $4)`,
		pid, role, scopeKind, scopeID); err != nil {
		t.Fatalf("insert grant: %v", err)
	}
	return tok
}
