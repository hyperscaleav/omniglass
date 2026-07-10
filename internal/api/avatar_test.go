package api_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// onePxPNGB64 is a valid 8x8 solid-white PNG, base64-encoded. Built with the stdlib
// encoder (not a hand-pasted literal) so the bytes always decode; the server
// normalizes it to a 256x256 JPEG on upload.
var onePxPNGB64 = func() string {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}()

// TestSelfSetAndReadAvatar drives the self avatar write path: a signed-in human
// uploads a picture (204), GET /auth/me then reports has_avatar true, and a remove
// (204) flips it back to false. The read endpoint (GET /auth/me/avatar) lands in a
// later slice. Skipped under -short.
func TestSelfSetAndReadAvatar(t *testing.T) {
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
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()

	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// No picture to start.
	if _, body := c.send(ownerTok, "GET", "/auth/me", nil); bytes.Contains(body, []byte(`"has_avatar":true`)) {
		t.Fatalf("has_avatar true before any upload: %s", body)
	}
	// Self-set a valid PNG; the read model then reports the flag.
	c.do(ownerTok, "POST", "/auth/me:setAvatar", map[string]string{"image_base64": onePxPNGB64}, http.StatusNoContent)
	if _, body := c.send(ownerTok, "GET", "/auth/me", nil); !bytes.Contains(body, []byte(`"has_avatar":true`)) {
		t.Fatalf("has_avatar not set after upload: %s", body)
	}
	// Self-remove; the flag clears.
	c.do(ownerTok, "POST", "/auth/me:removeAvatar", nil, http.StatusNoContent)
	if _, body := c.send(ownerTok, "GET", "/auth/me", nil); bytes.Contains(body, []byte(`"has_avatar":true`)) {
		t.Fatalf("has_avatar still set after remove: %s", body)
	}
}

// TestSelfSetAvatar_RejectsGarbage proves the normalize guard rejects a non-image
// upload with a 422 (not a 500), so a bad upload is a client error. Skipped under -short.
func TestSelfSetAvatar_RejectsGarbage(t *testing.T) {
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
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()

	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	garbage := base64.StdEncoding.EncodeToString([]byte("hello"))
	c.do(ownerTok, "POST", "/auth/me:setAvatar", map[string]string{"image_base64": garbage}, http.StatusUnprocessableEntity)
}

// TestAdminSetAvatar_GatedByPermission proves the admin avatar routes are gated by
// principal:set-avatar: a viewer (which lacks it) is a 403, while an owner sets
// another human's picture (204) and the directory then reports has_avatar true, and
// a remove (204) clears it. Skipped under -short.
func TestAdminSetAvatar_GatedByPermission(t *testing.T) {
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
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()

	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	viewerTok := principalWithGrants(t, ctx, dsn, "viewer-only", []grant{{role: "viewer", scopeKind: "all"}})
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Owner creates a human target.
	created := c.do(ownerTok, "POST", "/principals", map[string]string{"username": "bob"}, http.StatusCreated)
	var bob struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created, &bob); err != nil || bob.ID == "" {
		t.Fatalf("create bob: %v (%s)", err, created)
	}

	// A viewer lacks principal:set-avatar: a capability fast-reject 403.
	c.do(viewerTok, "POST", "/principals/"+bob.ID+":setAvatar", map[string]string{"image_base64": onePxPNGB64}, http.StatusForbidden)

	// The owner sets it; the directory then shows has_avatar true.
	c.do(ownerTok, "POST", "/principals/"+bob.ID+":setAvatar", map[string]string{"image_base64": onePxPNGB64}, http.StatusNoContent)
	if _, body := c.send(ownerTok, "GET", "/principals/"+bob.ID, nil); !bytes.Contains(body, []byte(`"has_avatar":true`)) {
		t.Fatalf("bob has_avatar not set after admin upload: %s", body)
	}

	// The owner removes it; the flag clears.
	c.do(ownerTok, "POST", "/principals/"+bob.ID+":removeAvatar", nil, http.StatusNoContent)
	if _, body := c.send(ownerTok, "GET", "/principals/"+bob.ID, nil); bytes.Contains(body, []byte(`"has_avatar":true`)) {
		t.Fatalf("bob has_avatar still set after admin remove: %s", body)
	}
}
