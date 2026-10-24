---
title: "files slice 1, a content-addressed blob store and a tenant-wide file handle"
date: 2026-07-14
status: "Accepted"
summary: "Files slice 1: a content-addressed `blob` store primitive (pgblobs) and a tenant-wide `file` handle; no placement arc (a file is 1:many, its locality is a future attachment), a binary `sensitive` flag reusing the secret `:admin` tier (defaults off), a delete frees its unreferenced blob synchronously (async mark-sweep GC deferred), base64-in-JSON on the wire"
---

- **Date:** 2026-07-14 | **Status:** Accepted | **Pages:** [files and blobs](/architecture/files/), [storage](/architecture/storage/), [identity and access](/architecture/identity-access/)
- **Decision:** The files subsystem ships its first slice: a content-addressed **`blob`** store as a Storage
  Gateway primitive (a `blob.Store` seam, default **pgblobs** backend holding bytes inline, keyed by the sha256
  of the bytes, dedup via `on conflict do nothing`, integrity-verified on read), and a **`file`** handle,
  searchable metadata (name, content_type, size, sha256, sensitive) that points at a blob by hash, with CRUD
  over the API, the generated CLI, and the typed client, plus the Files directory (under Values; see the
  [ADR-0026 update](#adr-0026-console-nav-ia-estate-values-get-their-own-top-level-group-the-settings-group-becomes-admin)). Four calls
  shape it. **(1) No placement arc on a file.** A file is tenant-wide, not on the exclusive arc a secret sits on,
  because a file relates **1:many** (to entities and types) rather than 1:1; that locality is a future
  many-to-many **attachment**, not an owner column, so the gateway injects no ABAC tree scope on a file query.
  (This reverses an in-design proposal to give `file` a secret-style owner scope.) **(2) Sensitivity reuses the
  secret mechanism, binary, defaulting off.** A per-file `sensitive` flag reuses
  [ADR-0025](#adr-0025-secret-is-a-sensitive-resource-a-per-secret-admin_sensitive-flag-flips-a-secret-to-the-admin-tier)'s
  `:admin`-tier rule (hidden from a lister without the tier, a non-disclosing 404 to a reader without it,
  admin-only to create), but defaults **false** (a file is shared unless marked, where a secret defaults sensitive
  because it is a credential), and `file` is **not** added to the sensitive-resource set, so the viewer floor
  (`*:read`) reads ordinary files. **(3) A delete frees its blob synchronously; async GC deferred.** `DeleteFile`
  drops the handle and, in the same transaction, frees the blob **when no other handle references it** (a dedup-aware
  refcount: a deleted file reclaims its bytes rather than leaking storage, but a blob shared by another handle is
  kept). The general async mark-sweep GC (for blobs referenced by other things, an aged large log body, a
  `collection.failed` raw, an attach event, none of which exist yet) stays a later slice; today a `file` is the only
  referencer, so the synchronous check is complete. **(4) One backend, base64-in-JSON on the wire.** Only the pgblobs backend ships (S3 and disk
  behind the same seam later); upload and download carry the bytes **base64 in JSON**, reusing the avatar precedent
  ([ADR-0018](#adr-0018-the-avatar-read-endpoint-is-json-not-raw-image-bytes)) so the whole surface stays under the
  Huma authz middleware and generates a uniform client and CLI. content_type lives on the **file**, not the blob:
  content-addressing is about the bytes, so identical bytes are one blob regardless of declared type.
- **Context:** [files.md](/architecture/files/) specified the two-layer model (handle plus content-addressed blob)
  and an index-probe GC; its open questions (inline-versus-blob threshold, chunking, the grace floor) are untouched
  here. The **1:many** insight is what separated a file's *locality* (attachment, deferred) from its *access*
  (permission plus sensitivity), and is why the file does not copy the secret owner arc. A full
  **classification + clearance** lattice (an ordered ladder on the resource, a clearance on the principal, an
  external-principal class) was considered for the sensitive axis and split into
  [its own epic (#243)](https://github.com/hyperscaleav/omniglass/issues/243) rather than inflating this slice; the
  binary flag is a 2-rung subset it will subsume. Multipart streaming for very large blobs is deferred with the
  S3/chunking slice.
- **Divergences logged:** files.md moved `content_type` from the blob to the file; the in-design file owner/scope
  arc was dropped (a file is off the placement arc). Both are reflected in the page.
- **Lands:** [epic #242](https://github.com/hyperscaleav/omniglass/issues/242), [#244](https://github.com/hyperscaleav/omniglass/issues/244).
