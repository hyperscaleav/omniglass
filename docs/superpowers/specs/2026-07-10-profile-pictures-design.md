# Profile pictures (avatars)

Status: approved design, pre-implementation. One vertical slice: API + CLI + UI + docs.

## Goal

Give human principals a profile picture. Two management surfaces:

- **Self**: any signed-in user sets/removes their own avatar (self-scoped, no capability, like the original change-password lane).
- **Admin**: a privileged operator sets/removes any principal's avatar, gated by a new `principal:set-avatar` permission (all-scope).

Images are normalized and size-limited server-side, then stored base64 in a column on the user's row.

## Decisions (locked)

1. Compression/resize is **server-side, authoritative** (Go). The client cannot bypass it.
2. Write transport is a **base64 JSON body** (`{ image_base64, content_type }`), not multipart. Generator-friendly for the typed SPA client and the cobra CLI.
3. Read transport is **raw JPEG bytes** from a dedicated endpoint; list/detail JSON carries only `has_avatar bool` (no b64 payload bloat per row).
4. **One** new permission: `principal:set-avatar` (admin). Self management needs no capability.
5. Image spec: accept JPEG/PNG/WebP (non-animated); **center-crop to square, resize 256x256, re-encode JPEG q82**; one stored size (~30-50KB b64).

## Storage

Additive dbmate migration (idempotent, never edits an applied migration). On the `human` table:

- `avatar text` - nullable, base64-encoded normalized JPEG.
- `avatar_updated_at timestamptz` - nullable, set on write, cleared on remove; drives the read ETag.

Storage Gateway (`internal/storage/iam.go`):

- `SetHumanAvatar(ctx, actorID, principalID string, jpegB64 string, scope ...)` - writes avatar + `avatar_updated_at = now()`, audited.
- `ClearHumanAvatar(ctx, actorID, principalID string, scope ...)` - nulls both, audited.
- `GetHumanAvatar(ctx, principalID string) (b64 string, updatedAt time.Time, ok bool, err error)` - read for the bytes endpoint.
- `HumanProfile` gains `HasAvatar bool` and `AvatarUpdatedAt *time.Time`; the avatar bytes are NOT loaded into the profile struct (kept off the hot list/detail path).

Admin writes carry ABAC scope (`principal:set-avatar` scope); self writes address the caller's own row directly.

## Image primitive (primitive-first)

New pure package `internal/avatar`:

```
func Normalize(raw []byte) (jpeg []byte, err error)
```

- Reject `len(raw) > 8 MiB` before decode (decompression-bomb guard on input).
- Decode JPEG/PNG/WebP. Reject anything else and animated GIF (unsupported format error).
- Reject any dimension > 8000px (bomb guard on decoded pixels).
- Center-crop to a square (min of width/height), resize to 256x256 with `golang.org/x/image/draw` (CatmullRom).
- Re-encode JPEG quality 82. Return bytes.

Pure, no I/O, no clock. Typed errors so the API maps them to 422 (bad/oversize image) vs 500 (encode failure).

**Tests** (unit, golden fixtures under `internal/avatar/testdata/`): valid JPEG/PNG/WebP normalize to 256x256 JPEG; oversize payload rejected; oversize dimensions rejected; non-image rejected; animated GIF rejected; non-square input is center-cropped (assert output is square). This is the capability-wrapping unit; the real-image round-trip is the gate.

## Write API (base64 JSON in)

New Huma routes (source of truth; OpenAPI + clients generated via `make gen`):

- `POST /principals/{id}:setAvatar` - admin. Middleware `authn + require("principal","set-avatar")`. Body `{ image_base64 (required), content_type (optional hint) }`. Handler: base64-decode, `avatar.Normalize`, base64-encode result, `SetHumanAvatar`. 422 on bad base64 or bad image. Target may be any human (including self); no takeover guard (an avatar is not a capability). Audited, actor = caller.
- `POST /principals/{id}:removeAvatar` - admin, same gate. `ClearHumanAvatar`. Idempotent (removing an absent avatar is 204/no-op, not 404).
- `POST /auth/me:setAvatar` - self. Middleware `authn` only (self-scoped, in the ungated self-service allow-list alongside update-me and change-password). Writes the caller's own row.
- `POST /auth/me:removeAvatar` - self. Same self lane.

Both `:setAvatar` return the updated `has_avatar` state (or 204). Route descriptions follow the house voice; each names its gate and the 422/403 conditions.

## Read API (raw bytes out)

- `GET /principals/{id}/avatar` - returns raw `image/jpeg` with `ETag` (derived from `avatar_updated_at`) and `Cache-Control: private, must-revalidate`; `304` on matching `If-None-Match`; `404` when null. Auth via session; reading another principal's avatar is gated `principal:read`.
- `GET /auth/me/avatar` - the caller's own avatar, same headers, self lane.

These return non-JSON, so they are consumed by the browser `<img src>` directly (same-origin session cookie), not through the typed JSON client. The base64 stored value is decoded to bytes at serve time.

`has_avatar bool` is added to the principal read models (`GET /principals/{id}`, the Users list view, `GET /auth/me`) so the UI knows whether to render an `<img>` or a fallback.

## CLI

Falls out of the generator once the routes exist:

- `omniglass principals set-avatar --id <id> --image-base64 <b64>` and a `--image-file <path>` convenience flag that reads + encodes locally.
- `omniglass principals remove-avatar --id <id>`.

Verify `cligen` handles the `:setAvatar` custom-method + JSON body (it wires custom methods already); if the file-flag convenience needs hand-holding, add it in the CLI layer, not the generator.

## UI (SolidJS + daisyUI)

- **Profile page** (`web/src/pages/Profile.tsx`): show current avatar (daisyUI `avatar`, fallback to initials/placeholder when `!has_avatar`); file picker reads the file, client may downscale for preview but the server is authoritative; POST `/auth/me:setAvatar`; remove button; live preview; error surface for 422 (bad/oversize image). Cache-bust the `<img>` on `avatar_updated_at` change.
- **Users page** (`web/src/pages/Users.tsx`): per-row avatar thumbnail (or placeholder); admin edit blade gains upload + remove, rendered only when the caller holds `principal:set-avatar`.
- **Top nav**: show the signed-in user's avatar. Include if the slice stays manageable; otherwise defer to a follow-up issue (no scope creep into the slice).

Screenshots (Profile manage, Users thumbnails + admin blade) ship in the PR per docs-with-everything.

## Docs (docs-with-everything)

- Identity/access page: add `principal:set-avatar` to the permission catalog; document the self lane (no capability) and the admin lane; explain the server-side normalize pipeline and the bomb guards.
- Status badge bump to its new floor; `status.mdx` build-progress note; decision-log entry for any divergence from the page's prior design.

## Testing

- **Unit**: `internal/avatar` golden-fixture suite (above).
- **Integration** (testcontainers Postgres): `SetHumanAvatar` / `ClearHumanAvatar` / `GetHumanAvatar` round-trip; `avatar_updated_at` set and cleared; audit rows written.
- **End-to-end (API)**: self set/remove via `/auth/me`; admin set/remove via `/principals/{id}` with and without `principal:set-avatar` (403 without); read endpoint returns bytes + correct `Content-Type` + `ETag`, `304` on revalidate, `404` when absent; `has_avatar` reflected in read models; bad/oversize image → 422.
- `make test` green before PR (pasted into `/ship-slice`).

## Out of scope (YAGNI)

- Multiple stored sizes / responsive srcset (one 256x256).
- Gravatar or external avatar sources.
- Avatars for non-human principals (service/node/agent).
- A `principal:set-own-avatar` capability (self needs none).
- Animated avatars.
