package api_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	_ "image/jpeg" // registers the JPEG decoder so image.DecodeConfig recognizes the stored avatar
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

// TestSelfSetAndReadAvatar drives the self avatar round-trip: a signed-in human
// uploads a picture (204), GET /auth/me reports has_avatar true, GET /auth/me/avatar
// returns the normalized JPEG as base64, and a remove (204) flips the flag back to
// false and turns the read endpoint into a 404. Skipped under -short.
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
	// Read the picture back: a 200 with a non-empty image_base64 that decodes to a
	// JPEG (the server normalized the PNG upload to a 256x256 JPEG).
	read := c.do(ownerTok, "GET", "/auth/me/avatar", nil, http.StatusOK)
	var got struct {
		ImageBase64 string `json:"image_base64"`
	}
	if err := json.Unmarshal(read, &got); err != nil || got.ImageBase64 == "" {
		t.Fatalf("read avatar: err=%v body=%s", err, read)
	}
	jpegBytes, err := base64.StdEncoding.DecodeString(got.ImageBase64)
	if err != nil {
		t.Fatalf("image_base64 is not valid base64: %v", err)
	}
	if _, format, err := image.DecodeConfig(bytes.NewReader(jpegBytes)); err != nil || format != "jpeg" {
		t.Fatalf("stored image format = %q (err %v), want jpeg", format, err)
	}
	// Self-remove; the flag clears and the read endpoint becomes a 404.
	c.do(ownerTok, "POST", "/auth/me:removeAvatar", nil, http.StatusNoContent)
	if _, body := c.send(ownerTok, "GET", "/auth/me", nil); bytes.Contains(body, []byte(`"has_avatar":true`)) {
		t.Fatalf("has_avatar still set after remove: %s", body)
	}
	c.do(ownerTok, "GET", "/auth/me/avatar", nil, http.StatusNotFound)
}

// TestGetAvatar_404WhenAbsent proves the read endpoint is a 404 for a principal
// that has never set a picture (not an empty 200). Skipped under -short.
func TestGetAvatar_404WhenAbsent(t *testing.T) {
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

	c.do(ownerTok, "GET", "/auth/me/avatar", nil, http.StatusNotFound)
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
	// The owner reads it back through the admin route: a 200 with a non-empty picture.
	read := c.do(ownerTok, "GET", "/principals/"+bob.ID+"/avatar", nil, http.StatusOK)
	var got struct {
		ImageBase64 string `json:"image_base64"`
	}
	if err := json.Unmarshal(read, &got); err != nil || got.ImageBase64 == "" {
		t.Fatalf("admin read bob avatar: err=%v body=%s", err, read)
	}

	// The owner removes it; the flag clears and the read route becomes a 404.
	c.do(ownerTok, "POST", "/principals/"+bob.ID+":removeAvatar", nil, http.StatusNoContent)
	if _, body := c.send(ownerTok, "GET", "/principals/"+bob.ID, nil); bytes.Contains(body, []byte(`"has_avatar":true`)) {
		t.Fatalf("bob has_avatar still set after admin remove: %s", body)
	}
	c.do(ownerTok, "GET", "/principals/"+bob.ID+"/avatar", nil, http.StatusNotFound)
}

// TestGetPrincipalAvatar_ScopedReaderForbidden proves the admin avatar read route
// enforces ABAC scope, not just the principal:read capability. A location-scoped
// admin HOLDS principal:read (via the admin role) so it clears the capability gate,
// but the gateway's all-scope invariant must still refuse it (403) on another
// principal's avatar, exactly as GET /principals/{id} does. An all-scope owner reads
// the same picture (200). Skipped under -short.
func TestGetPrincipalAvatar_ScopedReaderForbidden(t *testing.T) {
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
	// A location-scoped admin holds principal:read (via the admin role) but only at a
	// narrow scope, so it passes the capability gate yet must be refused by the
	// gateway's all-scope invariant, exactly as on GET /principals/{id}.
	scopedTok := principalWithGrants(t, ctx, dsn, "hq-admin", []grant{{role: "admin", scopeKind: "location", scopeID: "HQ"}})
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Owner creates a human target and sets its picture.
	created := c.do(ownerTok, "POST", "/principals", map[string]string{"username": "carol"}, http.StatusCreated)
	var carol struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created, &carol); err != nil || carol.ID == "" {
		t.Fatalf("create carol: %v (%s)", err, created)
	}
	c.do(ownerTok, "POST", "/principals/"+carol.ID+":setAvatar", map[string]string{"image_base64": onePxPNGB64}, http.StatusNoContent)

	// The all-scope owner reads it back: a 200 with a non-empty picture.
	read := c.do(ownerTok, "GET", "/principals/"+carol.ID+"/avatar", nil, http.StatusOK)
	var got struct {
		ImageBase64 string `json:"image_base64"`
	}
	if err := json.Unmarshal(read, &got); err != nil || got.ImageBase64 == "" {
		t.Fatalf("owner read carol avatar: err=%v body=%s", err, read)
	}

	// The scoped admin passes the principal:read capability but is refused by the
	// gateway's all-scope invariant: a 403, never a 200 leaking the picture.
	c.do(scopedTok, "GET", "/principals/"+carol.ID+"/avatar", nil, http.StatusForbidden)
}
