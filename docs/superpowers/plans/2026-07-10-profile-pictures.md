# Profile Pictures Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give human principals a profile picture, self-managed by any signed-in user and admin-managed via a new `principal:set-avatar` permission, normalized and size-limited server-side, stored base64 on the user's row.

**Architecture:** A pure `internal/avatar.Normalize` primitive decodes/crops/resizes/re-encodes any upload to a 256x256 JPEG. Storage adds two columns to `human` and gateway methods that write/clear/read the base64 blob with audit. The Huma API gains write custom-methods (self + admin) and JSON read endpoints; `has_avatar` rides the existing principal read models. The CLI and typed SPA client fall out of `make gen`. The SolidJS console adds image-backed avatars that fetch the blob and render a data URL.

**Tech Stack:** Go, Huma v2, chi, pgx/Postgres (dbmate), `golang.org/x/image/draw` + `/webp`, testcontainers-go, SolidJS + daisyUI, openapi-typescript, cobra (generated).

## Global Constraints

- No em dashes in any artifact (code comments, docs, commit messages); use commas/colons/periods/parentheses.
- No AI/assistant attribution anywhere.
- PR title + commit subjects: conventional-commit type, lowercase first letter after the prefix (`feat: profile pictures ...`).
- Migrations run once, are never edited after applying, and are idempotent (`add column if not exists`). New migration timestamp must exceed `20260709160000`.
- Storage is the only DB path; outbound effects go through the gateway. Every gateway method is added in THREE places: the `storage.Gateway` interface (`internal/storage/storage.go`), the `*PG` impl (`internal/storage/iam.go`), and `UnimplementedGateway` (`internal/storage/unimplemented.go`).
- API-first: Huma structs are the source of truth. After any route change run `make gen` and commit the regenerated `api/openapi.{json,yaml}`, `internal/cli/api_gen.go`, `web/src/api/schema.gen.ts`.
- Authorization is an invariant: every admin route carries `a.authn` + `a.require(resource, action)`; self routes carry `a.authn` only and resolve the caller from the session. Do NOT add a chi-native handler that bypasses the Huma authz middleware.
- Image spec: accept JPEG/PNG/WebP (GIF and everything else rejected); reject a base64 payload whose decoded bytes exceed 8 MiB or any dimension exceeds 8000px; center-crop to square; resize to 256x256; re-encode JPEG quality 82.
- `internal/storage/iam.go:loadPrincipal` runs on EVERY authenticated request. It must select `avatar is not null` (a bool), never the avatar bytes.
- Validate locally with `make test-short` while iterating and `make test` before the PR. No `--no-verify`.

---

### Task 1: The `internal/avatar` normalize primitive

**Files:**
- Create: `internal/avatar/avatar.go`
- Create: `internal/avatar/avatar_test.go`
- Create fixtures: `internal/avatar/testdata/` (generated in Step 1)
- Modify: `go.mod` / `go.sum` (adds `golang.org/x/image`)

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `func Normalize(raw []byte) (jpeg []byte, err error)`
  - Sentinel errors: `var ErrTooLarge = errors.New("avatar: image too large")`, `var ErrUnsupported = errors.New("avatar: unsupported image")`. Callers map `ErrTooLarge`/`ErrUnsupported` to HTTP 422.

- [ ] **Step 1: Generate golden fixtures**

Write a throwaway Go program (or a `TestMain`-free helper) that emits fixtures, or generate them inline in the test's `init`. Simplest: create them with a tiny script committed alongside. Create `internal/avatar/testdata/` with:
- `red_600x400.png` - a 600x400 solid red PNG (non-square, to prove center-crop).
- `blue_100x100.jpg` - a 100x100 solid blue JPEG.
- `green_300x300.webp` - a 300x300 WebP (if a WebP encoder is inconvenient, generate via the `chai2010/webp` throwaway or ship a checked-in file).
- `notanimage.txt` - arbitrary bytes.
- `anim.gif` - a 2-frame animated GIF.

Generate the two easy ones in Go and commit them:

```go
// internal/avatar/testdata/gen/main.go  (run once: `go run ./internal/avatar/testdata/gen`, then delete)
package main

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
)

func main() {
	red := image.NewRGBA(image.Rect(0, 0, 600, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 600; x++ {
			red.Set(x, y, color.RGBA{200, 30, 30, 255})
		}
	}
	f, _ := os.Create("internal/avatar/testdata/red_600x400.png")
	_ = png.Encode(f, red)
	_ = f.Close()

	blue := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			blue.Set(x, y, color.RGBA{30, 30, 200, 255})
		}
	}
	g, _ := os.Create("internal/avatar/testdata/blue_100x100.jpg")
	_ = jpeg.Encode(g, blue, &jpeg.Options{Quality: 90})
	_ = g.Close()
}
```

For `anim.gif` write a 2-frame gif with `image/gif` `EncodeAll`; for `notanimage.txt` write `[]byte("not an image at all")`; for the WebP fixture, if no encoder is handy, defer the WebP assertion to a `t.Skip` guarded by `os.Stat` (do NOT block the task on WebP fixture tooling, but DO keep the WebP decode path wired). Commit the fixtures.

- [ ] **Step 2: Write the failing test**

```go
// internal/avatar/avatar_test.go
package avatar_test

import (
	"bytes"
	"errors"
	"image"
	"os"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/avatar"
)

func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func TestNormalize_PNGProducesSquareJPEG(t *testing.T) {
	out, err := avatar.Normalize(mustRead(t, "red_600x400.png"))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if format != "jpeg" {
		t.Errorf("format = %q, want jpeg", format)
	}
	if cfg.Width != 256 || cfg.Height != 256 {
		t.Errorf("size = %dx%d, want 256x256", cfg.Width, cfg.Height)
	}
}

func TestNormalize_JPEGPassesThrough(t *testing.T) {
	if _, err := avatar.Normalize(mustRead(t, "blue_100x100.jpg")); err != nil {
		t.Fatalf("normalize jpeg: %v", err)
	}
}

func TestNormalize_RejectsNonImage(t *testing.T) {
	if _, err := avatar.Normalize(mustRead(t, "notanimage.txt")); !errors.Is(err, avatar.ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

func TestNormalize_RejectsGIF(t *testing.T) {
	if _, err := avatar.Normalize(mustRead(t, "anim.gif")); !errors.Is(err, avatar.ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

func TestNormalize_RejectsOversizePayload(t *testing.T) {
	big := make([]byte, 9<<20) // 9 MiB
	if _, err := avatar.Normalize(big); !errors.Is(err, avatar.ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
}
```

- [ ] **Step 3: Run the test, verify it fails**

Run: `go test ./internal/avatar/...`
Expected: FAIL - `undefined: avatar.Normalize`.

- [ ] **Step 4: Add the dependency**

Run: `go get golang.org/x/image/draw@latest`
Expected: `go.mod` gains `golang.org/x/image`.

- [ ] **Step 5: Implement `Normalize`**

```go
// internal/avatar/avatar.go
// Package avatar normalizes an uploaded image into a fixed-size square JPEG for
// use as a principal's profile picture. It is pure: decode, guard, crop, resize,
// re-encode, with no I/O. The guards (payload cap, dimension cap) make it safe to
// run on untrusted uploads.
package avatar

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"

	// Decoders registered for image.Decode / image.DecodeConfig. GIF is
	// deliberately NOT registered, so a GIF (animated or not) fails to decode and
	// is rejected as unsupported.
	_ "image/png"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	maxBytes = 8 << 20 // 8 MiB decoded-payload cap (decompression-bomb guard on input)
	maxDim   = 8000    // reject any source dimension above this (bomb guard on pixels)
	outSize  = 256     // output is outSize x outSize
	quality  = 82
)

var (
	ErrTooLarge    = errors.New("avatar: image too large")
	ErrUnsupported = errors.New("avatar: unsupported image")
)

// Normalize decodes raw (JPEG, PNG, or WebP), center-crops it to a square,
// resizes to 256x256, and re-encodes as JPEG. Oversize payloads and dimensions
// are rejected with ErrTooLarge; anything that is not a supported image is
// ErrUnsupported.
func Normalize(raw []byte) ([]byte, error) {
	if len(raw) > maxBytes {
		return nil, ErrTooLarge
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, ErrUnsupported
	}
	if cfg.Width > maxDim || cfg.Height > maxDim {
		return nil, ErrTooLarge
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, ErrUnsupported
	}

	// Center-crop to the largest centered square.
	b := src.Bounds()
	side := b.Dx()
	if b.Dy() < side {
		side = b.Dy()
	}
	ox := b.Min.X + (b.Dx()-side)/2
	oy := b.Min.Y + (b.Dy()-side)/2
	crop := image.Rect(ox, oy, ox+side, oy+side)

	dst := image.NewRGBA(image.Rect(0, 0, outSize, outSize))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, draw.Over, nil)

	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("avatar: encode: %w", err)
	}
	return out.Bytes(), nil
}
```

Note: `image.Decode` needs a JPEG decoder registered; it is pulled in transitively by `image/jpeg` (imported for `Encode`). PNG and WebP are the blank imports above.

- [ ] **Step 6: Run the tests, verify they pass**

Run: `go test ./internal/avatar/...`
Expected: PASS (WebP test skipped if no fixture).

- [ ] **Step 7: Commit**

```bash
git add internal/avatar go.mod go.sum
git commit -m "feat: avatar normalize primitive (crop, resize, re-encode)"
```

---

### Task 2: Storage columns + profile read of `has_avatar`

**Files:**
- Create: `db/migrations/20260710120000_human_avatar.sql`
- Modify: `internal/storage/iam.go` (the `HumanProfile` struct ~line 183; the `loadPrincipal` human branch ~line 1163)
- Create/Modify test: `internal/storage/iam_avatar_test.go`

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces: `storage.HumanProfile` gains `HasAvatar bool` and `AvatarUpdatedAt *time.Time`. `loadPrincipal` populates both from the `human` row. (No avatar bytes on this path.)

- [ ] **Step 1: Write the migration**

```sql
-- db/migrations/20260710120000_human_avatar.sql
-- migrate:up
-- A human gains an optional profile picture: a base64-encoded 256x256 JPEG the
-- server normalizes on upload, plus the time it last changed (drives cache and
-- the "has avatar" read flag). Both nullable; a human without a picture falls
-- back to initials in the console.
alter table human add column if not exists avatar text;
alter table human add column if not exists avatar_updated_at timestamptz;

-- migrate:down
alter table human drop column if exists avatar_updated_at;
alter table human drop column if exists avatar;
```

- [ ] **Step 2: Write the failing test**

```go
// internal/storage/iam_avatar_test.go
package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func newAvatarGW(t *testing.T) (context.Context, storage.Gateway, string) {
	t.Helper()
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("new pg: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := gw.UpsertRole(ctx, storage.Role{ID: "owner", Official: true, Permissions: []string{"*:*"}}); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	zeros := make([]byte, 32)
	if err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "ops", SecretHash: zeros, Prefix: "abcd1234"}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	pr, err := gw.AuthenticateBearer(ctx, zeros)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	return ctx, gw, pr.ID
}

func TestLoadPrincipal_NoAvatarByDefault(t *testing.T) {
	ctx, gw, pid := newAvatarGW(t)
	pr, err := gw.GetPrincipal(ctx, pid, scopeAll())
	if err != nil {
		t.Fatalf("get principal: %v", err)
	}
	if pr.Human == nil || pr.Human.HasAvatar {
		t.Errorf("HasAvatar = %v, want false", pr.Human.HasAvatar)
	}
	if pr.Human.AvatarUpdatedAt != nil {
		t.Errorf("AvatarUpdatedAt = %v, want nil", pr.Human.AvatarUpdatedAt)
	}
}
```

`scopeAll()` helper: reuse the existing one in the package if present (grep `scope.Set{All: true}` in `internal/storage/*_test.go`); otherwise add `func scopeAll() scope.Set { return scope.Set{All: true} }` in this file with `import "github.com/hyperscaleav/omniglass/internal/scope"`.

- [ ] **Step 3: Run, verify it fails**

Run: `go test ./internal/storage/ -run TestLoadPrincipal_NoAvatarByDefault`
Expected: FAIL - compile error `pr.Human.HasAvatar undefined`.

- [ ] **Step 4: Add struct fields**

In `internal/storage/iam.go`, extend `HumanProfile` (around line 183):

```go
type HumanProfile struct {
	Username, Email, DisplayName string
	MustChangePassword           bool
	// HasAvatar is true when the avatar column is non-null. The avatar bytes are
	// NOT loaded here (this struct is filled on every authenticated request);
	// fetch them with GetHumanAvatar.
	HasAvatar       bool
	AvatarUpdatedAt *time.Time
}
```

Confirm `time` is imported in `iam.go` (it is, used elsewhere).

- [ ] **Step 5: Extend `loadPrincipal`**

In the `case "human":` branch (around line 1163), change the query and scan to select the two cheap derived values (never the bytes):

```go
	case "human":
		var h HumanProfile
		if err := p.pool.QueryRow(ctx,
			`select username, coalesce(email, ''), coalesce(display_name, ''), must_change_password,
			        avatar is not null, avatar_updated_at
			 from human where principal_id = $1`,
			pr.ID).Scan(&h.Username, &h.Email, &h.DisplayName, &h.MustChangePassword,
			&h.HasAvatar, &h.AvatarUpdatedAt); err != nil {
			return fmt.Errorf("storage: load human: %w", err)
		}
		pr.Human = &h
```

- [ ] **Step 6: Run, verify it passes**

Run: `go test ./internal/storage/ -run TestLoadPrincipal_NoAvatarByDefault`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add db/migrations/20260710120000_human_avatar.sql internal/storage/iam.go internal/storage/iam_avatar_test.go
git commit -m "feat: human avatar columns and has-avatar read flag"
```

---

### Task 3: Gateway avatar write/clear/read methods

**Files:**
- Modify: `internal/storage/storage.go` (interface, near `SetPrincipalPassword` ~line 123)
- Modify: `internal/storage/iam.go` (the `*PG` methods)
- Modify: `internal/storage/unimplemented.go` (stubs)
- Modify: `internal/storage/iam_avatar_test.go`

**Interfaces:**
- Consumes: `scope.Set` (already imported in these files).
- Produces (added to `storage.Gateway`):
  - `SetOwnAvatar(ctx context.Context, principalID, jpegB64 string) error`
  - `ClearOwnAvatar(ctx context.Context, principalID string) error`
  - `SetPrincipalAvatar(ctx context.Context, actorID, id, jpegB64 string, action scope.Set) error`
  - `ClearPrincipalAvatar(ctx context.Context, actorID, id string, action scope.Set) error`
  - `GetHumanAvatar(ctx context.Context, id string) (jpegB64 string, ok bool, err error)`
  - Reuses `ErrPrincipalForbidden` / `ErrPrincipalNotFound`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/storage/iam_avatar_test.go`:

```go
func TestSetGetClearOwnAvatar(t *testing.T) {
	ctx, gw, pid := newAvatarGW(t)
	if err := gw.SetOwnAvatar(ctx, pid, "AAAA"); err != nil {
		t.Fatalf("set own: %v", err)
	}
	b64, ok, err := gw.GetHumanAvatar(ctx, pid)
	if err != nil || !ok || b64 != "AAAA" {
		t.Fatalf("get = (%q,%v,%v), want (AAAA,true,nil)", b64, ok, err)
	}
	pr, _ := gw.GetPrincipal(ctx, pid, scopeAll())
	if !pr.Human.HasAvatar || pr.Human.AvatarUpdatedAt == nil {
		t.Errorf("HasAvatar=%v updatedAt=%v, want true/non-nil", pr.Human.HasAvatar, pr.Human.AvatarUpdatedAt)
	}
	if err := gw.ClearOwnAvatar(ctx, pid); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, ok, _ := gw.GetHumanAvatar(ctx, pid); ok {
		t.Errorf("ok = true after clear, want false")
	}
}

func TestSetPrincipalAvatar_RequiresAllScope(t *testing.T) {
	ctx, gw, pid := newAvatarGW(t)
	if err := gw.SetPrincipalAvatar(ctx, pid, pid, "AAAA", scope.Set{}); err != storage.ErrPrincipalForbidden {
		t.Errorf("err = %v, want ErrPrincipalForbidden", err)
	}
	if err := gw.SetPrincipalAvatar(ctx, pid, pid, "AAAA", scope.Set{All: true}); err != nil {
		t.Errorf("admin set with all-scope: %v", err)
	}
}
```

Add `import "github.com/hyperscaleav/omniglass/internal/scope"` to the test file if not present.

- [ ] **Step 2: Run, verify it fails**

Run: `go test ./internal/storage/ -run 'Avatar'`
Expected: FAIL - `gw.SetOwnAvatar undefined`.

- [ ] **Step 3: Add interface declarations**

In `internal/storage/storage.go`, near `SetPrincipalPassword` (~line 123):

```go
	SetOwnAvatar(ctx context.Context, principalID, jpegB64 string) error
	ClearOwnAvatar(ctx context.Context, principalID string) error
	SetPrincipalAvatar(ctx context.Context, actorID, id, jpegB64 string, action scope.Set) error
	ClearPrincipalAvatar(ctx context.Context, actorID, id string, action scope.Set) error
	GetHumanAvatar(ctx context.Context, id string) (string, bool, error)
```

- [ ] **Step 4: Implement on `*PG`**

Add to `internal/storage/iam.go` (a private helper plus the five methods):

```go
// setAvatar writes (or clears, when b64 == "") a human's avatar within tx and
// audits the change against actorID. It does not commit.
func setAvatarTx(ctx context.Context, tx pgx.Tx, actorID, id, b64 string) error {
	var val, ts any
	verb := "clear_avatar"
	if b64 != "" {
		val, ts, verb = b64, "now()", "set_avatar"
	}
	// avatar_updated_at is set to now() on write and null on clear. Using a
	// literal keeps the value server-clocked.
	if _, err := tx.Exec(ctx,
		`update human set avatar = $2, avatar_updated_at = case when $3 then now() else null end
		 where principal_id = $1`,
		id, val, b64 != ""); err != nil {
		return fmt.Errorf("storage: write avatar: %w", err)
	}
	_ = ts
	return writeAuditRes(ctx, tx, actorID, verb, "principal", id, nil, nil)
}

func (p *PG) SetOwnAvatar(ctx context.Context, principalID, jpegB64 string) error {
	return p.avatarTxn(ctx, principalID, principalID, jpegB64)
}

func (p *PG) ClearOwnAvatar(ctx context.Context, principalID string) error {
	return p.avatarTxn(ctx, principalID, principalID, "")
}

func (p *PG) SetPrincipalAvatar(ctx context.Context, actorID, id, jpegB64 string, action scope.Set) error {
	if !action.All {
		return ErrPrincipalForbidden
	}
	return p.avatarTxn(ctx, actorID, id, jpegB64)
}

func (p *PG) ClearPrincipalAvatar(ctx context.Context, actorID, id string, action scope.Set) error {
	if !action.All {
		return ErrPrincipalForbidden
	}
	return p.avatarTxn(ctx, actorID, id, "")
}

func (p *PG) avatarTxn(ctx context.Context, actorID, id, b64 string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin avatar: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists int
	if err := tx.QueryRow(ctx, `select 1 from human where principal_id = $1`, id).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPrincipalNotFound
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "22P02" {
			return ErrPrincipalNotFound
		}
		return fmt.Errorf("storage: avatar lookup: %w", err)
	}
	if err := setAvatarTx(ctx, tx, actorID, id, b64); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit avatar: %w", err)
	}
	return nil
}

func (p *PG) GetHumanAvatar(ctx context.Context, id string) (string, bool, error) {
	var b64 *string
	if err := p.pool.QueryRow(ctx, `select avatar from human where principal_id = $1`, id).Scan(&b64); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, ErrPrincipalNotFound
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "22P02" {
			return "", false, ErrPrincipalNotFound
		}
		return "", false, fmt.Errorf("storage: get avatar: %w", err)
	}
	if b64 == nil {
		return "", false, nil
	}
	return *b64, true, nil
}
```

Remove the dead `ts`/`_ = ts` scaffolding: simplify `setAvatarTx` to drop the unused `ts` variable (kept above only to show the intent; the actual `update` already server-clocks via `now()`), like this final form:

```go
func setAvatarTx(ctx context.Context, tx pgx.Tx, actorID, id, b64 string) error {
	verb := "clear_avatar"
	var val any
	if b64 != "" {
		val, verb = b64, "set_avatar"
	}
	if _, err := tx.Exec(ctx,
		`update human set avatar = $2, avatar_updated_at = case when $3 then now() else null end
		 where principal_id = $1`,
		id, val, b64 != ""); err != nil {
		return fmt.Errorf("storage: write avatar: %w", err)
	}
	return writeAuditRes(ctx, tx, actorID, verb, "principal", id, nil, nil)
}
```

Confirm `errors`, `pgx`, `pgconn` are already imported in `iam.go` (they are, used by `SetPrincipalPassword`).

- [ ] **Step 5: Add `UnimplementedGateway` stubs**

In `internal/storage/unimplemented.go`, near the `SetPrincipalPassword` stub (~line 108):

```go
func (UnimplementedGateway) SetOwnAvatar(context.Context, string, string) error { return nil }
func (UnimplementedGateway) ClearOwnAvatar(context.Context, string) error       { return nil }
func (UnimplementedGateway) SetPrincipalAvatar(context.Context, string, string, string, scope.Set) error {
	return nil
}
func (UnimplementedGateway) ClearPrincipalAvatar(context.Context, string, string, scope.Set) error {
	return nil
}
func (UnimplementedGateway) GetHumanAvatar(context.Context, string) (string, bool, error) {
	return "", false, nil
}
```

- [ ] **Step 6: Run, verify it passes**

Run: `go test ./internal/storage/ -run 'Avatar'`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/storage/storage.go internal/storage/iam.go internal/storage/unimplemented.go internal/storage/iam_avatar_test.go
git commit -m "feat: gateway methods to set, clear, and read a human avatar"
```

---

### Task 4: API write routes + `has_avatar` on read models

**Files:**
- Modify: `internal/api/principals.go` (input structs; `toPrincipalBody` ~line 35; `registerPrincipalRoutes` ~line 119; `humanBody`/`principalBody`)
- Modify: `internal/api/auth.go` (`humanBody` ~line 396; `meHandler` ~line 427; new self handlers)
- Modify: `internal/api/api.go` (register self routes ~line 131)
- Create test: `internal/api/avatar_test.go` (follow the existing API E2E test harness used by `principals_test.go`)

**Interfaces:**
- Consumes: `avatar.Normalize` (Task 1), the five gateway methods (Task 3), `pr.Human.HasAvatar` (Task 2).
- Produces: `humanBody` gains `HasAvatar bool json:"has_avatar"`. New OperationIDs: `set-principal-avatar`, `remove-principal-avatar`, `set-auth-me-avatar`, `remove-auth-me-avatar`.

- [ ] **Step 1: Write the failing E2E tests**

Model these on the existing API test harness (open `internal/api/principals_test.go` to copy the server/bootstrap/login helper - reuse it verbatim; do not invent a new harness). The assertions:

```go
// internal/api/avatar_test.go  (package api_test, harness helpers reused from principals_test.go)
//
// A 1x1 PNG, base64. Generate once and paste the constant, or build inline with
// image/png in a helper. onePxPNGB64 below is a valid 1x1 white PNG.
const onePxPNGB64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M8AAAMBAQDJ/pL+AAAAAElFTkSuQmCC"

func TestSelfSetAndReadAvatar(t *testing.T) {
	// bootstrap owner, login, get a session client (helper from principals_test.go)
	// POST /auth/me:setAvatar {image_base64: onePxPNGB64} -> 204
	// GET  /auth/me -> body.human.has_avatar == true
	// GET  /auth/me/avatar -> 200 {image_base64: <non-empty jpeg b64>}
	// POST /auth/me:removeAvatar -> 204
	// GET  /auth/me -> has_avatar == false
}

func TestSelfSetAvatar_RejectsGarbage(t *testing.T) {
	// POST /auth/me:setAvatar {image_base64: base64("hello")} -> 422
}

func TestAdminSetAvatar_GatedByPermission(t *testing.T) {
	// create a second human with only the viewer role (no principal:set-avatar)
	// as that user, POST /principals/{ownerId}:setAvatar -> 403
	// as owner, POST /principals/{otherId}:setAvatar -> 204, GET /principals/{otherId} has_avatar true
}
```

Fill each test body using the concrete request helper from `principals_test.go` (it already does bootstrap + `POST /auth/login` + cookie-bearing requests for reset-password tests; mirror that). Keep the four scenarios above.

- [ ] **Step 2: Run, verify it fails**

Run: `go test ./internal/api/ -run Avatar`
Expected: FAIL - routes 404 / handlers undefined.

- [ ] **Step 3: Add `has_avatar` to the read models**

In `internal/api/auth.go`, extend the shared `humanBody` (~line 396):

```go
type humanBody struct {
	Username           string `json:"username"`
	Email              string `json:"email,omitempty"`
	DisplayName        string `json:"display_name,omitempty"`
	MustChangePassword bool   `json:"must_change_password,omitempty" doc:"..."`
	HasAvatar          bool   `json:"has_avatar,omitempty" doc:"True when the principal has a profile picture; fetch it from the avatar endpoint."`
}
```

In `internal/api/principals.go` `toPrincipalBody` (~line 38), set it:

```go
	if pr.Human != nil {
		b.Human = &humanBody{Username: pr.Human.Username, Email: pr.Human.Email, DisplayName: pr.Human.DisplayName, HasAvatar: pr.Human.HasAvatar}
	}
```

In `internal/api/auth.go` `meHandler` (~line 427), set it:

```go
	if pr.Human != nil {
		out.Body.Human = &humanBody{
			Username:           pr.Human.Username,
			Email:              pr.Human.Email,
			DisplayName:        pr.Human.DisplayName,
			MustChangePassword: pr.Human.MustChangePassword,
			HasAvatar:          pr.Human.HasAvatar,
		}
	}
```

- [ ] **Step 4: Add the admin write routes**

In `internal/api/principals.go`, add input structs near `resetPasswordInput` (~line 67):

```go
type setAvatarInput struct {
	ID   string `path:"id" doc:"The principal, addressed by its uuid or a human username"`
	Body struct {
		ImageBase64 string `json:"image_base64" doc:"The image (JPEG, PNG, or WebP), base64-encoded; normalized server-side to a 256x256 JPEG"`
	}
}

type avatarPathInput struct {
	ID string `path:"id" doc:"The principal, addressed by its uuid or a human username"`
}
```

In `registerPrincipalRoutes` (~line 119), register the three routes (set, remove, read). Handlers mirror the reset-password structure (resolve ref, scope, gateway):

```go
	huma.Register(api, huma.Operation{
		OperationID:   "set-principal-avatar",
		Method:        http.MethodPost,
		Path:          "/principals/{id}:setAvatar",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Set a principal's profile picture",
		Description:   "Sets another human principal's profile picture (an administrator action). Gated by principal:set-avatar (all-scope). The image (JPEG, PNG, or WebP, base64-encoded) is normalized server-side to a 256x256 JPEG; a bad or oversize image is a 422. Audited with the administrator as the actor.",
		Middlewares:   huma.Middlewares{a.authn, a.require("principal", "set-avatar")},
	}, func(ctx context.Context, in *setAvatarInput) (*struct{}, error) {
		id, rerr := a.resolvePrincipalRef(ctx, in.ID)
		if rerr != nil {
			return nil, rerr
		}
		b64, aerr := normalizeAvatar(in.Body.ImageBase64)
		if aerr != nil {
			return nil, aerr
		}
		if err := gw.SetPrincipalAvatar(ctx, actorID(ctx), id, b64, a.scopeFor(ctx, "principal", "set-avatar")); err != nil {
			return nil, mapPrincipalErr(err)
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "remove-principal-avatar",
		Method:        http.MethodPost,
		Path:          "/principals/{id}:removeAvatar",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Remove a principal's profile picture",
		Description:   "Clears another human principal's profile picture. Gated by principal:set-avatar (all-scope). Removing an absent picture is a no-op. Audited with the administrator as the actor.",
		Middlewares:   huma.Middlewares{a.authn, a.require("principal", "set-avatar")},
	}, func(ctx context.Context, in *avatarPathInput) (*struct{}, error) {
		id, rerr := a.resolvePrincipalRef(ctx, in.ID)
		if rerr != nil {
			return nil, rerr
		}
		if err := gw.ClearPrincipalAvatar(ctx, actorID(ctx), id, a.scopeFor(ctx, "principal", "set-avatar")); err != nil {
			return nil, mapPrincipalErr(err)
		}
		return nil, nil
	})
```

The read route is added in Task 5 (kept separate so its tests gate independently).

Add the shared normalize+map helper at the bottom of `internal/api/principals.go`:

```go
// normalizeAvatar decodes the base64 upload, normalizes the image, and returns
// it re-encoded as base64 for storage. Bad base64 or a bad/oversize image is a
// 422; anything else is a 500.
func normalizeAvatar(imageB64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(imageB64)
	if err != nil {
		return "", huma.Error422UnprocessableEntity("image_base64 is not valid base64")
	}
	jpeg, err := avatar.Normalize(raw)
	if err != nil {
		if errors.Is(err, avatar.ErrTooLarge) || errors.Is(err, avatar.ErrUnsupported) {
			return "", huma.Error422UnprocessableEntity(err.Error())
		}
		return "", huma.Error500InternalServerError("normalize avatar")
	}
	return base64.StdEncoding.EncodeToString(jpeg), nil
}
```

Add imports to `internal/api/principals.go`: `"encoding/base64"`, `"errors"`, `"github.com/hyperscaleav/omniglass/internal/avatar"`.

- [ ] **Step 5: Add the self write routes**

In `internal/api/auth.go`, add the input + handlers:

```go
type setAvatarMeInput struct {
	Body struct {
		ImageBase64 string `json:"image_base64" doc:"The image (JPEG, PNG, or WebP), base64-encoded; normalized server-side to a 256x256 JPEG"`
	}
}

func (a *authenticator) setMeAvatarHandler(ctx context.Context, in *setAvatarMeInput) (*struct{}, error) {
	pr, ok := principalFrom(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("unauthenticated")
	}
	b64, err := normalizeAvatar(in.Body.ImageBase64)
	if err != nil {
		return nil, err
	}
	if err := a.gw.SetOwnAvatar(ctx, pr.ID, b64); err != nil {
		return nil, huma.Error500InternalServerError("set avatar")
	}
	return nil, nil
}

func (a *authenticator) removeMeAvatarHandler(ctx context.Context, _ *struct{}) (*struct{}, error) {
	pr, ok := principalFrom(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("unauthenticated")
	}
	if err := a.gw.ClearOwnAvatar(ctx, pr.ID); err != nil {
		return nil, huma.Error500InternalServerError("remove avatar")
	}
	return nil, nil
}
```

In `internal/api/api.go`, register them next to `change-auth-me-password` (~line 143), authn-only:

```go
	huma.Register(api, huma.Operation{
		OperationID:   "set-auth-me-avatar",
		Method:        http.MethodPost,
		Path:          "/auth/me:setAvatar",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Set your own profile picture",
		Description:   "Sets the caller's profile picture (JPEG, PNG, or WebP, base64-encoded), normalized server-side to a 256x256 JPEG. Requires authentication; self-scoped. A bad or oversize image is a 422.",
		Middlewares:   huma.Middlewares{a.authn},
	}, a.setMeAvatarHandler)

	huma.Register(api, huma.Operation{
		OperationID:   "remove-auth-me-avatar",
		Method:        http.MethodPost,
		Path:          "/auth/me:removeAvatar",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Remove your own profile picture",
		Description:   "Clears the caller's profile picture. Requires authentication; self-scoped.",
		Middlewares:   huma.Middlewares{a.authn},
	}, a.removeMeAvatarHandler)
```

Note: these are deliberately NOT added to the `MustChangePassword` allow-list in `authn` (a user under a forced password change stays gated to the change-password lane).

- [ ] **Step 6: Run, verify the write + has_avatar tests pass**

Run: `go test ./internal/api/ -run 'Avatar'`
Expected: the set/remove/gated tests PASS; the `GET /auth/me/avatar` read assertion still FAILS (route added in Task 5). Split the read assertion into Task 5's test if it blocks; keep write scenarios green here.

- [ ] **Step 7: Commit**

```bash
git add internal/api/principals.go internal/api/auth.go internal/api/api.go internal/api/avatar_test.go
git commit -m "feat: API routes to set and remove a principal avatar (self and admin)"
```

---

### Task 5: API avatar read routes

**Files:**
- Modify: `internal/api/principals.go` (read route in `registerPrincipalRoutes`)
- Modify: `internal/api/auth.go` (self read handler)
- Modify: `internal/api/api.go` (register `GET /auth/me/avatar`)
- Modify: `internal/api/avatar_test.go`

**Interfaces:**
- Consumes: `gw.GetHumanAvatar` (Task 3).
- Produces: `avatarOutput` struct `{ Body struct{ ImageBase64 string } }`. OperationIDs `get-principal-avatar`, `get-auth-me-avatar`.

- [ ] **Step 1: Write the failing test**

In `internal/api/avatar_test.go`, complete the read assertions in `TestSelfSetAndReadAvatar` (fetch `GET /auth/me/avatar`, assert 200 + non-empty `image_base64` that decodes to a JPEG) and add:

```go
func TestGetAvatar_404WhenAbsent(t *testing.T) {
	// fresh owner, no avatar set
	// GET /auth/me/avatar -> 404
}
```

- [ ] **Step 2: Run, verify it fails**

Run: `go test ./internal/api/ -run Avatar`
Expected: FAIL - `GET /auth/me/avatar` 404s for the set case (route missing).

- [ ] **Step 3: Add the output struct + admin read route**

In `internal/api/principals.go`:

```go
type avatarOutput struct {
	Body struct {
		ImageBase64 string `json:"image_base64" doc:"The profile picture as a base64-encoded 256x256 JPEG"`
	}
}
```

Register in `registerPrincipalRoutes`:

```go
	huma.Register(api, huma.Operation{
		OperationID: "get-principal-avatar",
		Method:      http.MethodGet,
		Path:        "/principals/{id}/avatar",
		Summary:     "Get a principal's profile picture",
		Description: "Returns the principal's profile picture as a base64-encoded JPEG. Gated by principal:read. A principal without a picture is a 404.",
		Middlewares: huma.Middlewares{a.authn, a.require("principal", "read")},
	}, func(ctx context.Context, in *avatarPathInput) (*avatarOutput, error) {
		id, rerr := a.resolvePrincipalRef(ctx, in.ID)
		if rerr != nil {
			return nil, rerr
		}
		b64, ok, err := gw.GetHumanAvatar(ctx, id)
		if err != nil {
			return nil, mapPrincipalErr(err)
		}
		if !ok {
			return nil, huma.Error404NotFound("no profile picture")
		}
		out := &avatarOutput{}
		out.Body.ImageBase64 = b64
		return out, nil
	})
```

- [ ] **Step 4: Add the self read route**

In `internal/api/auth.go`:

```go
func (a *authenticator) meAvatarHandler(ctx context.Context, _ *struct{}) (*avatarOutput, error) {
	pr, ok := principalFrom(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("unauthenticated")
	}
	b64, has, err := a.gw.GetHumanAvatar(ctx, pr.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("get avatar")
	}
	if !has {
		return nil, huma.Error404NotFound("no profile picture")
	}
	out := &avatarOutput{}
	out.Body.ImageBase64 = b64
	return out, nil
}
```

Register in `internal/api/api.go` (authn-only, and add its OperationID to the `MustChangePassword` allow-list ONLY if you want a forced-change user to preview their own avatar - default: leave it gated, so do NOT add it):

```go
	huma.Register(api, huma.Operation{
		OperationID: "get-auth-me-avatar",
		Method:      http.MethodGet,
		Path:        "/auth/me/avatar",
		Summary:     "Get your own profile picture",
		Description: "Returns the caller's profile picture as a base64-encoded JPEG. Requires authentication; self-scoped. No picture is a 404.",
		Middlewares: huma.Middlewares{a.authn},
	}, a.meAvatarHandler)
```

- [ ] **Step 5: Run, verify it passes**

Run: `go test ./internal/api/ -run Avatar`
Expected: PASS (all avatar scenarios).

- [ ] **Step 6: Commit**

```bash
git add internal/api/principals.go internal/api/auth.go internal/api/api.go internal/api/avatar_test.go
git commit -m "feat: API routes to read a principal avatar (self and admin)"
```

---

### Task 6: Regenerate clients (OpenAPI, CLI, TS)

**Files:**
- Modify (generated): `api/openapi.json`, `api/openapi.yaml`, `internal/cli/api_gen.go`, `web/src/api/schema.gen.ts`

**Interfaces:**
- Consumes: the routes from Tasks 4-5.
- Produces: generated `api.POST("/principals/{id}:setAvatar")` etc. in the TS client, and `omniglass principal setAvatar <id> --image-base64` in the CLI.

- [ ] **Step 1: Run the generator chain**

Run: `make gen`
Expected: the four generated files change. `internal/cli/api_gen.go` gains `setAvatar`/`removeAvatar`/`avatar` commands under `principal`; `schema.gen.ts` gains the new paths.

- [ ] **Step 2: Verify the build**

Run: `go build ./... && go test ./internal/cli/...`
Expected: PASS. Confirm the CLI command exists: `go run ./cmd/omniglass principal --help` lists `setAvatar`.

- [ ] **Step 3: Commit**

```bash
git add api/openapi.json api/openapi.yaml internal/cli/api_gen.go web/src/api/schema.gen.ts
git commit -m "chore: regenerate OpenAPI, CLI, and typed client for avatars"
```

---

### Task 7: Profile page self-manage UI

**Files:**
- Modify: `web/src/lib/auth.ts` (add `setMyAvatar`, `removeMyAvatar`, `fetchMyAvatar` wrappers)
- Modify: `web/src/pages/Profile.tsx` (image-backed avatar + upload/remove controls)
- Create/Modify test: `web/src/pages/Profile.test.tsx`

**Interfaces:**
- Consumes: `api.POST("/auth/me:setAvatar")`, `api.POST("/auth/me:removeAvatar")`, `api.GET("/auth/me/avatar")`, `me.data.human.has_avatar`.
- Produces: an `AvatarImg` render pattern (data-URL-backed daisyUI avatar) reused in Task 8.

- [ ] **Step 1: Add the client wrappers**

In `web/src/lib/auth.ts`, mirror the `changePassword` shape (returns `{ ok, message }`):

```ts
// Read a File as a base64 string (no data: prefix).
export function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const s = String(reader.result);
      resolve(s.slice(s.indexOf(",") + 1));
    };
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(file);
  });
}

export async function setMyAvatar(file: File): Promise<{ ok: boolean; message: string }> {
  const image_base64 = await fileToBase64(file);
  const { error } = await api.POST("/auth/me:setAvatar", { body: { image_base64 } });
  return error ? { ok: false, message: (error as { detail?: string }).detail ?? "Upload failed." } : { ok: true, message: "" };
}

export async function removeMyAvatar(): Promise<{ ok: boolean; message: string }> {
  const { error } = await api.POST("/auth/me:removeAvatar", {});
  return error ? { ok: false, message: "Remove failed." } : { ok: true, message: "" };
}

export async function fetchMyAvatar(): Promise<string | null> {
  const { data, error } = await api.GET("/auth/me/avatar", {});
  if (error || !data) return null;
  return `data:image/jpeg;base64,${data.image_base64}`;
}
```

- [ ] **Step 2: Write the failing test**

In `web/src/pages/Profile.test.tsx` (follow `Users.test.tsx` for the render/mocks harness), assert: when `me.data.human.has_avatar` is true the profile renders an `<img>` (not the initials placeholder); when false it renders initials; clicking Remove calls the remove endpoint. Mock `api` as `Users.test.tsx` does.

Run: `cd web && npm test -- Profile`
Expected: FAIL.

- [ ] **Step 3: Implement the avatar section in `Profile.tsx`**

Replace the initials-only preview block (`Profile.tsx:92-103`) with an image-when-present avatar plus upload/remove controls. Use a `createResource`/signal to hold the fetched data URL:

```tsx
const [avatarUrl, { refetch: refetchAvatar }] = createResource(
  () => human()?.has_avatar ?? false,
  (has) => (has ? fetchMyAvatar() : Promise.resolve(null)),
);
const [avatarMsg, setAvatarMsg] = createSignal<Note>(null);

async function onPickAvatar(e: Event) {
  const file = (e.currentTarget as HTMLInputElement).files?.[0];
  if (!file) return;
  const r = await setMyAvatar(file);
  if (r.ok) {
    await me.refetch?.();      // refresh has_avatar
    await refetchAvatar();
    setAvatarMsg({ tone: "success", text: "Profile picture updated." });
  } else {
    setAvatarMsg({ tone: "error", text: r.message });
  }
}

async function onRemoveAvatar() {
  const r = await removeMyAvatar();
  if (r.ok) {
    await me.refetch?.();
    await refetchAvatar();
    setAvatarMsg(null);
  } else {
    setAvatarMsg({ tone: "error", text: r.message });
  }
}
```

Render (the first image-backed daisyUI avatar in the app):

```tsx
<div class="flex items-center gap-3">
  <Show
    when={avatarUrl()}
    fallback={
      <div class="avatar avatar-placeholder">
        <div class="w-16 rounded-full bg-linear-to-br from-primary to-info text-primary-content">
          <span class="font-data text-lg font-bold uppercase">{initials()}</span>
        </div>
      </div>
    }
  >
    <div class="avatar">
      <div class="w-16 rounded-full">
        <img src={avatarUrl()!} alt="Your profile picture" />
      </div>
    </div>
  </Show>
  <div class="flex flex-col gap-1">
    <label class="btn btn-sm btn-outline">
      Upload
      <input type="file" accept="image/jpeg,image/png,image/webp" class="hidden" onChange={onPickAvatar} />
    </label>
    <Show when={human()?.has_avatar}>
      <button class="btn btn-sm btn-ghost" onClick={onRemoveAvatar}>Remove</button>
    </Show>
  </div>
</div>
<Note note={avatarMsg()} />
```

Add imports: `createResource`, `Show` from `solid-js`; `setMyAvatar`, `removeMyAvatar`, `fetchMyAvatar` from `../lib/auth`. Keep the daisyUI `avatar` markup exactly (Kobalte not needed; the file input inside a `<label>` is a native control, not an interactive Kobalte trigger, so it does not hit the label-trigger gotcha).

- [ ] **Step 4: Run, verify it passes**

Run: `cd web && npm test -- Profile`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/auth.ts web/src/pages/Profile.tsx web/src/pages/Profile.test.tsx
git commit -m "feat: self-manage profile picture on the profile page"
```

---

### Task 8: Users list thumbnail + admin manage UI

**Files:**
- Modify: `web/src/lib/principals.ts` (add `setPrincipalAvatar`, `removePrincipalAvatar`, `principalAvatarUrl`)
- Modify: `web/src/pages/Users.tsx` (list name-cell renders an image when `p.human?.has_avatar`)
- Modify: `web/src/pages/UserDetail.tsx` (admin avatar panel, gated `can(me.data, "principal", "set-avatar")`)
- Modify test: `web/src/pages/Users.test.tsx`

**Interfaces:**
- Consumes: `api.POST("/principals/{id}:setAvatar")`, `:removeAvatar`, `api.GET("/principals/{id}/avatar")`, `p.human.has_avatar`, `can(...)` from `web/src/lib/auth.ts`, `fileToBase64` from Task 7.
- Produces: nothing downstream.

- [ ] **Step 1: Add the client wrappers**

In `web/src/lib/principals.ts` (mirror `resetPrincipalPassword`):

```ts
export async function setPrincipalAvatar(id: string, file: File): Promise<void> {
  const image_base64 = await fileToBase64(file);
  const { error } = await api.POST("/principals/{id}:setAvatar", { params: { path: { id } }, body: { image_base64 } });
  if (error) throw error;
}

export async function removePrincipalAvatar(id: string): Promise<void> {
  const { error } = await api.POST("/principals/{id}:removeAvatar", { params: { path: { id } } });
  if (error) throw error;
}

export async function principalAvatarUrl(id: string): Promise<string | null> {
  const { data, error } = await api.GET("/principals/{id}/avatar", { params: { path: { id } } });
  if (error || !data) return null;
  return `data:image/jpeg;base64,${data.image_base64}`;
}
```

Import `fileToBase64` from `./auth`.

- [ ] **Step 2: Write the failing tests**

In `web/src/pages/Users.test.tsx`, add: a row whose principal has `human.has_avatar: true` renders an `<img>`; a row without renders initials. Mock `principalAvatarUrl` to resolve a stub data URL.

Run: `cd web && npm test -- Users`
Expected: FAIL.

- [ ] **Step 3: Implement the list thumbnail**

In `Users.tsx` name-cell (`:36-49`), wrap the placeholder in a `Show` on `p.human?.has_avatar`, lazily loading the URL via a small per-row `createResource`. Keep the initials block as the fallback:

```tsx
cell: (p) => <UserAvatarCell p={p} />,
```

Add a `UserAvatarCell` component in `Users.tsx` (or a shared `web/src/components/UserAvatar.tsx` if cleaner) that renders the image-backed avatar when `p.human?.has_avatar`, fetching `principalAvatarUrl(p.id)` in a `createResource`, else the existing initials placeholder. Reuse the exact daisyUI `avatar` markup from Task 7 at `w-7`.

- [ ] **Step 4: Implement the admin panel in `UserDetail.tsx`**

Mirror the reset-password panel (`UserDetail.tsx:259-274`) and its kebab gating (`:107-109`). Add:

```tsx
const canSetAvatar = () => can(me.data, "principal", "set-avatar");
```

In the edit blade, when `pr.human && canSetAvatar()`, render an avatar row with the current picture (fetched via `principalAvatarUrl(pr.id)`), an Upload `<label><input type="file" .../></label>` calling `setPrincipalAvatar(pr.id, file)`, and a Remove button (shown when `pr.human.has_avatar`) calling `removePrincipalAvatar(pr.id)`. On success, refetch the principal (the blade already reloads via its detail query - reuse that) and the avatar URL. Route errors to the existing `setActErr`.

- [ ] **Step 5: Run, verify it passes**

Run: `cd web && npm test -- Users`
Expected: PASS. Then `cd web && npm run build` to confirm no type errors against `schema.gen.ts`.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/principals.ts web/src/pages/Users.tsx web/src/pages/UserDetail.tsx web/src/pages/Users.test.tsx web/src/components/UserAvatar.tsx
git commit -m "feat: admin-manage avatars and list thumbnails on the users console"
```

---

### Task 9: Docs, status, and full validation

**Files:**
- Modify: `docs/src/content/docs/architecture/identity-access.md` (permission catalog + avatar pipeline)
- Modify: the relevant `status.mdx` build-progress note and the page status badge
- Modify: `docs/src/content/docs/architecture/decisions.md` (only if the build diverged from the page's prior design - e.g. the JSON read endpoint vs a raw-bytes one)

**Interfaces:** none (documentation).

- [ ] **Step 1: Document the capability + pipeline**

In `identity-access.md`, add `principal:set-avatar` to the permission catalog with a one-line description ("set or remove any principal's profile picture; self-management needs no capability"). Add a short subsection: the server-side normalize pipeline (accept JPEG/PNG/WebP; center-crop; 256x256 JPEG q82; 8 MiB / 8000px bomb guards), that images are stored base64 on the human row, and that the read side is a JSON endpoint the console renders as a data URL (chosen over raw image bytes to keep every route under the Huma authz middleware).

- [ ] **Step 2: Advance status**

Bump the page's status badge to its new floor (Partial or Built as appropriate) and add the `status.mdx` note for this slice. If the shipped read design (JSON `image_base64` endpoint) differs from any prior "raw bytes" design note, add a decision-log entry recording the divergence and the authz-invariant reason.

- [ ] **Step 3: Full test pass**

Run: `make test`
Expected: PASS (all tiers, real Postgres via testcontainers). Paste the output into the `/ship-slice` report.

- [ ] **Step 4: Gen-drift check**

Run: `make gen && git status --porcelain`
Expected: no changes (generation is idempotent; if anything changed, commit it).

- [ ] **Step 5: Commit**

```bash
git add docs
git commit -m "docs: document profile pictures and the set-avatar capability"
```

- [ ] **Step 6: Ship gate**

Run `/ship-slice`. It runs the fresh `make test`, the `make gen` drift check, the em-dash + attribution scan, a reviewer pass, and requires live screenshots of the Profile and Users surfaces (driven against `make dev`). Its emitted report becomes the PR body. Only then open the PR (`feat: profile pictures for human principals (self + admin manage)`, closes #174).

---

## Notes for the implementer

- The read endpoint is deliberately a Huma JSON route (`{ image_base64 }`), NOT a raw `image/jpeg` handler. A raw-bytes chi handler would bypass the Huma authz middleware (the "permission on every route" invariant) and would not authenticate token-only sessions behind a bare `<img src>`. The SPA fetches the JSON via the typed client (cookie + bearer both work) and renders a `data:` URL.
- Avatar bytes never load on the `loadPrincipal` hot path; only `has_avatar` (a boolean) and `avatar_updated_at` do.
- `principal:set-avatar` needs no `roles.yaml` edit: admin holds it via `principal:*` and owner via `>`. Verify by running the admin-gated E2E test (Task 4) green and the viewer-role 403 case.
- The only hand-written code is the primitive, the storage/API/handlers, and the UI; the CLI command and typed client are generated. Do not hand-edit `api_gen.go` or `schema.gen.ts`.
