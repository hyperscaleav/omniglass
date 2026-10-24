---
title: "The avatar read endpoint is JSON, not raw image bytes"
date: 2026-07-10
status: "Accepted"
summary: "A profile picture is read through a JSON `image_base64` endpoint the console renders as a data URL, not a raw `image/jpeg` handler, so every route stays under the Huma authz middleware"
---

- **Date:** 2026-07-10 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** A human principal's profile picture is read through a **JSON** endpoint
  (`GET /principals/{id}/avatar` gated `principal:read`, `GET /auth/me/avatar` on the self lane) that returns
  `{ image_base64 }`, which the console decodes into a `data:` URL for the `<img>`. The write lanes take base64
  JSON in (`POST /principals/{id}:setAvatar` and the `/auth/me` self lane), and the server-normalized 256x256
  JPEG is stored base64 on the `human` row; the principal read models carry only a `has_avatar` bool, so no
  image payload rides a list or the `loadPrincipal` hot path.
- **Context:** The slice design spec proposed a **raw `image/jpeg`** read endpoint (with `ETag` /
  `Cache-Control` / `304`) so a browser `<img src>` could load it directly. But a raw-bytes handler would be a
  chi-native route sitting **outside** the Huma authz middleware, breaking the two-layer invariant that a
  `<resource>:<action>` capability is checked on **every** route, and a bare `<img src>` cannot send a bearer
  header, so a token-only (non-cookie) session could not authenticate the image. Keeping the read as a Huma
  JSON route puts it under the same `authn` + `require("principal","read")` (admin) or authn-only (self) path
  as every other route, and the typed client (session cookie or bearer, both work) fetches the JSON and builds
  the data URL. The one normalized size is small (roughly 30 to 50 KB base64), so per-request payload is not a
  concern, and HTTP caching over `avatar_updated_at` is a later refinement if it is ever needed. This
  supersedes the spec's raw-bytes read decision; the write transport (base64 JSON) is unchanged.
