---
title: "Files: the content-addressed blob store and the file handle"
date: 2026-07-14
refs: []
form: bullet
---

- **Files: the content-addressed blob store and the file handle.** The first
  [files](/architecture/files/) slice. A **`blob`** store lands as a Storage Gateway **primitive**: a
  `blob.Store` seam with a default **pgblobs** backend (bytes held inline in Postgres), keyed by the **sha256**
  of the bytes so identical bytes **dedup** to one row (`on conflict do nothing`), integrity-verified on read.
  On top of it, the **`file`** handle, searchable metadata (name, content_type, size, sha256, sensitive) that
  points at a blob by hash, gets CRUD over the API: **create-from-upload** hashes and dedups server-side, plus
  get, list, **download** (bytes read back and hash-verified), and delete (which **frees the blob** in the same
  transaction when no other handle still references it, dedup-aware, so storage is reclaimed; async mark-sweep GC of
  aged/event-referenced blobs is a later slice). Access is two layers with **no new machinery**: the `file:<action>` permission, and a per-file
  **`sensitive`** flag reusing the secret `:admin`-tier rule (a flagged file is hidden from a non-admin lister
  and a non-disclosing 404 to a non-admin reader, admin-only to create), defaulting **off** and leaving `file`
  off the sensitive-resource set so the viewer floor reads ordinary files. Files carry **no placement scope** (a
  file is tenant-wide; its 1:many locality is a future attachment, not an owner arc). The generated CLI (a `file`
  command group) and typed client follow from the Huma structs, and the **Files directory** ships under Values
  (upload, download, delete, a sensitive badge). Proven by the pure blob and file-validation unit tests, the
  **pgblobs testcontainer** round-trip and dedup gate (the capability-wrapping close), the gateway integration
  tests (round-trip, dedup, delete-leaves-blob, the sensitive gate), and a real-binary API e2e (upload to
  download identical bytes, the viewer/operator/admin permission and sensitive gates)
  ([ADR-0029](/architecture/decisions/#adr-0029-files-slice-1-a-content-addressed-blob-store-and-a-tenant-wide-file-handle),
  [#242](https://github.com/hyperscaleav/omniglass/issues/242), [#244](https://github.com/hyperscaleav/omniglass/issues/244)).
  Deferred: attach-to-entities-and-types (slice 2), tags-on-files ([#191](https://github.com/hyperscaleav/omniglass/issues/191)),
  async mark-sweep GC of aged/event-referenced blobs, the S3 and disk backends, and the classification lattice
  ([#243](https://github.com/hyperscaleav/omniglass/issues/243)).
