package api_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

type fileResp struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	Sensitive   bool   `json:"sensitive"`
}

// TestFileAPI drives the file surface over HTTP: create-from-upload computes the
// hash and dedups, download returns identical bytes, the directory lists, and the
// two authorization layers hold. A viewer reads ordinary files but cannot upload;
// an operator uploads and deletes ordinary files but cannot create a sensitive
// one nor see one (a non-disclosing 404); the admin tier (owner) can.
func TestFileAPI(t *testing.T) {
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

	// A location to scope the operator/viewer grants at (files ignore scope, but a
	// grant needs a target).
	locRaw := c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "hq", "location_type": "building"}, http.StatusCreated)
	var loc struct {
		ID string `json:"id"`
	}
	mustJSON(t, locRaw, &loc)
	operatorTok := setupScopedViewer(t, ctx, dsn, "op", "operator", "location", loc.ID)
	viewerTok := setupScopedViewer(t, ctx, dsn, "vw", "viewer", "location", loc.ID)

	payload := []byte("firmware image \x00\x01\x02 bytes")
	b64 := base64.StdEncoding.EncodeToString(payload)

	// Owner uploads an ordinary file; the server hashes and dedups.
	created := c.do(ownerTok, http.MethodPost, "/files", map[string]any{
		"name": "codec-fw.bin", "content_type": "application/octet-stream", "content": b64,
	}, http.StatusCreated)
	var f fileResp
	mustJSON(t, created, &f)
	if f.Size != int64(len(payload)) || f.SHA256 == "" {
		t.Fatalf("created file = %+v, want size %d and a hash", f, len(payload))
	}

	// Download returns identical bytes.
	dl := c.do(ownerTok, http.MethodGet, "/files/"+f.ID+":download", nil, http.StatusOK)
	var dlBody struct {
		Content string `json:"content"`
	}
	mustJSON(t, dl, &dlBody)
	got, err := base64.StdEncoding.DecodeString(dlBody.Content)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("download bytes = %q (err %v), want %q", got, err, payload)
	}

	// Viewer reads the directory but cannot upload.
	c.do(viewerTok, http.MethodGet, "/files", nil, http.StatusOK)
	c.do(viewerTok, http.MethodPost, "/files", map[string]any{
		"name": "x.txt", "content_type": "text/plain", "content": base64.StdEncoding.EncodeToString([]byte("x")),
	}, http.StatusForbidden)

	// Operator uploads an ordinary file, but a sensitive one is forbidden (no admin tier).
	c.do(operatorTok, http.MethodPost, "/files", map[string]any{
		"name": "notes.txt", "content_type": "text/plain", "content": base64.StdEncoding.EncodeToString([]byte("ops notes")),
	}, http.StatusCreated)
	c.do(operatorTok, http.MethodPost, "/files", map[string]any{
		"name": "quote.pdf", "content_type": "application/pdf", "content": base64.StdEncoding.EncodeToString([]byte("bid")), "sensitive": true,
	}, http.StatusForbidden)

	// Owner (admin tier) creates a sensitive file; it is invisible / non-disclosing to lesser callers.
	sensRaw := c.do(ownerTok, http.MethodPost, "/files", map[string]any{
		"name": "competitive-quote.pdf", "content_type": "application/pdf", "content": base64.StdEncoding.EncodeToString([]byte("winning bid")), "sensitive": true,
	}, http.StatusCreated)
	var sens fileResp
	mustJSON(t, sensRaw, &sens)
	if !sens.Sensitive {
		t.Fatal("created file not marked sensitive")
	}
	c.do(operatorTok, http.MethodGet, "/files/"+sens.ID, nil, http.StatusNotFound)
	c.do(viewerTok, http.MethodGet, "/files/"+sens.ID+":download", nil, http.StatusNotFound)
	c.do(ownerTok, http.MethodGet, "/files/"+sens.ID, nil, http.StatusOK)

	// Operator deletes its own ordinary file; a viewer cannot delete.
	c.do(viewerTok, http.MethodDelete, "/files/"+f.ID, nil, http.StatusForbidden)
	c.do(operatorTok, http.MethodDelete, "/files/"+f.ID, nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodGet, "/files/"+f.ID, nil, http.StatusNotFound)
}

func mustJSON(t *testing.T, raw []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
}
