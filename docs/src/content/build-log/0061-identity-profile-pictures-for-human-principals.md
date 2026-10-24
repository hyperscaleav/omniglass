---
title: "Identity: profile pictures for human principals"
date: 2026-07-10
refs: []
form: bullet
---

- **Identity: profile pictures for human principals.** A human principal can carry a **profile picture**,
  managed on two lanes: **self** (any signed-in user sets or removes their own via `POST /auth/me:setAvatar`
  / `:removeAvatar`, authn-only and self-scoped, no capability, on the ungated self-service lane) and
  **admin** (a new `principal:set-avatar` all-scope capability sets or removes any principal's via
  `POST /principals/{id}:setAvatar` / `:removeAvatar`, audited with the admin as actor; `admin` holds it
  through `principal:*` and `owner` through `>`, so no `roles.yaml` change). A pure `internal/avatar.Normalize`
  primitive is the server-authoritative pipeline: accept JPEG/PNG/WebP (GIF and all else rejected), reject a
  payload over 8 MiB or any dimension over 8000px (decompression-bomb guards), center-crop to a square, resize
  256x256, re-encode JPEG q82; a bad or oversize image is a 422. Two additive columns on `human` (`avatar`
  base64, `avatar_updated_at`) store the one normalized size; the bytes never load on the `loadPrincipal` hot
  path (which selects only `avatar is not null`), so a cheap `has_avatar` bool rides the read models
  (`GET /principals/{id}`, the Users list, `GET /auth/me`). The **read endpoint is JSON**
  (`GET /principals/{id}/avatar` gated `principal:read`, `GET /auth/me/avatar` self), returning
  `{ image_base64 }` the console renders as a `data:` URL, deliberately not a raw `image/jpeg` handler so every
  route stays under the Huma authz middleware
  ([ADR-0018](/architecture/decisions/#adr-0018-the-avatar-read-endpoint-is-json-not-raw-image-bytes)); a
  principal without a picture is a 404. The console **Profile** page gains an image-backed avatar with
  upload/remove (initials fallback), and the **Users** directory renders per-row thumbnails plus an admin
  upload/remove panel gated on `principal:set-avatar`. The CLI and typed client fall out of the generator
  (`omniglass principal setAvatar` / `removeAvatar`). Proven by the `internal/avatar` golden-fixture unit
  suite (a square JPEG out, non-image / GIF / oversize rejected), a storage round-trip (set / get / clear,
  `has_avatar` and `avatar_updated_at`, the all-scope gate on the admin write), and a real-binary API test
  (self set + read + remove, garbage 422, the admin permission 403 without `principal:set-avatar` and success
  with), plus web tests for both surfaces. Deferred: the top-nav avatar, non-human avatars, multiple sizes /
  srcset, and external (Gravatar) sources.
