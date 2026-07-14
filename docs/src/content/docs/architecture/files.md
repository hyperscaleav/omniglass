---
title: Files and blobs
description: A searchable file handle over a content-addressed blob store, behind the Storage Gateway.
sidebar:
  badge:
    text: Partial
    variant: note
---

:::note[Partial: the content-addressed blob store (pgblobs backend), the `file` handle CRUD (create-from-upload with sha256 dedup, get, list, download, delete), the per-file `sensitive` tier, and the Files directory under Inventory are built ([ADR-0029](/architecture/decisions/#adr-0029-files-slice-1-a-content-addressed-blob-store-and-a-tenant-wide-file-handle)); attachment, GC, alternate backends, and tags-on-files are deferred]
The first slice ([epic #242](https://github.com/hyperscaleav/omniglass/issues/242), [#244](https://github.com/hyperscaleav/omniglass/issues/244)) is **built**: the content-addressed **`blob`** store as a Storage Gateway primitive (default **pgblobs** backend, dedup on the hash), the **`file`** handle CRUD over the API, generated CLI, and typed client, and the Files operator directory. Access is two layers with no new machinery: the **`file:<action>`** permission plus a per-file **`sensitive`** flag reusing the secret sensitivity tier. Deferred to later slices: **attaching** a file to entities and types ([the many-to-many relation](https://github.com/hyperscaleav/omniglass/issues/242)), async mark-sweep **GC** of aged or event-referenced blobs (a file delete already frees its own blob synchronously when no other handle references it), the **S3** and **disk** backends behind the same seam, **tags-on-files** ([#191](https://github.com/hyperscaleav/omniglass/issues/191)), and search/filter beyond the basic list. The general **resource-classification + clearance** lattice that will subsume the binary `sensitive` flag is [its own epic (#243)](https://github.com/hyperscaleav/omniglass/issues/243).
:::

Files let an operator keep the opaque bytes that go with an estate, a firmware image, a config dump, a runbook, a packet capture, searchable and deduplicated, with a searchable **`file`** handle over a content-addressed **blob** store, behind the same Storage Gateway as everything else.

## Two layers: the file handle and the blob

- **`file`** is **indexable metadata**: name, content-type, size, `sha256`, tags. The searchable
  handle an operator references and finds (a firmware image, a device config dump, a runbook doc,
  a screenshot, a packet capture). It owns no bytes; it points at a blob by hash.
- **the blob store** holds the **bytes**, **content-addressed by `sha256`**. The hash is the key,
  so identical bytes are one blob.

Splitting them means search and inventory operations (list, filter, tag) never touch bytes, and
the same blob can back many file handles.

`file` tags reuse the `tag` **key** registry (the same tenant-wide governed vocabulary, so `category`
means the same thing on a firmware image as on a component, [config and credentials](/architecture/variables/)),
but bind as a **flat per-file set**: a file is not on the structural exclusive-arc, so there is no parent
to cascade from. The vocabulary is shared; the cascade is not.

## Access: a permission and a sensitive tier, no placement

A file carries **no estate placement**. Unlike a [secret](/architecture/variables/), which is *for* one
component or system and so sits on the exclusive arc, a file relates to *many* things (a firmware image
documents many device types) or to nothing (a loose upload), so its locality is its future
**attachments** (a many-to-many relation), not a single owner column. A file is therefore **tenant-wide**,
and two existing layers gate it, with no new machinery:

- the **`file:<action>`** permission on every route (`file:create`, `file:read`, `file:delete`; download
  rides `file:read`);
- a per-file **`sensitive`** flag that reuses the secret sensitivity tier
  ([ADR-0025](/architecture/decisions/#adr-0025-secret-is-a-sensitive-resource-a-per-secret-admin_sensitive-flag-flips-a-secret-to-the-admin-tier)): a flagged
  file is lifted to the **`:admin` tier** (`file:read:admin`), hidden from a lister without it and answered
  with a **non-disclosing 404** to a reader without it. So a competitive quote or a config dump with
  internal detail is a *sensitive global file*, admin/owner-only, while a firmware image or a runbook is an
  ordinary shared one.

Unlike a secret (which defaults sensitive, being a credential), the flag **defaults `false`**: a file is
shared unless marked. And unlike a secret, `file` is **not** in the sensitive-resource set, so the viewer
floor (`*:read`) reads ordinary files; only the flag fences the confidential ones. When the
[resource-classification + clearance](https://github.com/hyperscaleav/omniglass/issues/243) lattice lands,
it subsumes this binary flag (a 2-rung case of it) across files and secrets alike; the external-principal
class it introduces is what will keep an outside integrator from ever seeing an internal file.

## Content-addressing earns four properties

A blob is keyed by the hash of its bytes, not a UUID, which buys:

- **dedup**: identical bytes collapse to one blob (two operators uploading the same firmware, the
  same `raw` payload seen twice);
- **integrity**: the hash verifies the bytes on read, tamper-evident by construction;
- **immutability**: bytes cannot change without changing the key, like the append-only
  ground-truth logs;
- **backtest-stability**: an event referencing a hash still resolves under a backtest, because the hash is
  stable across a backtest.

So **rows reference a hash, never inline bytes.** Inline `bytea` would kill the hash-ref stability
property and bloat the firehose row. Small structured values (a datapoint, its labels) stay inline
in the row's jsonb; **large or opaque payloads become a blob hash-ref** (a dedicated **indexed**
`blob_sha256` column on the referencing row, so GC can probe it, not buried in jsonb): a big `log_datapoint`
body, and especially a **`collection.failed` event's raw** when the
wire payload is large (a full SNMP walk, a big HTTP body, a capture). Raw stays inline when small;
the size threshold is the switch.

:::caution[Open question]
The inline-versus-blob size threshold: one global cutoff, or per-kind (`raw` versus log body versus
operator upload).
:::

## Dedup is database-scoped

The blob key is **`sha256`**, the bare content hash. There is no `tenant_id`: isolation is
per-database (a database per tenant), so each tenant's blobs live in a separate database and dedup
is global *within* that database. One tenant can never detect another's content by hash collision,
because the blobs never share a store. The efficiency cost of not sharing bytes across databases is
the right price for physical isolation.

## Backends, swappable behind the gateway

The bytes live behind the Storage Gateway, so the backend swaps with no model change (the same
seam as the columnar and object tiers):

- **default: `pgblobs`** (a dedicated Postgres blob table), the single-binary,
  no-external-dependency story;
- **scale: an S3-compatible object store**;
- **disk** for local and dev.

The `file` and the hash reference are identical across backends; only `storage_ref` resolution
differs.

:::caution[Open question]
Chunking and streaming for very large blobs (firmware images, captures) on the `pgblobs` backend.
:::

## Reference-counted GC, not age-based

A blob is collectable **only when no live reference points at its hash AND a grace or retention
floor has passed**. Age-based GC alone is wrong: dedup means a blob uploaded long ago can be the
one a *recent* event references, so collecting by the blob's own age would orphan a live hash.
References come from:

- a **`file`** handle;
- a large `log_datapoint` body;
- a `collection.failed` raw hash-ref;
- an **attach event** (a `state_datapoint` or `audit_log` recording "this component was attached
  to this file at T").

References disappear two ways: a `file` is deleted, or a referencing **event ages out** (a
retention partition drop). So GC is **coupled to retention**: dropping a partition releases its
references, after which a now-unreferenced blob past the grace floor is collectable.

**Mechanism: index-probe mark-sweep by default.** GC enumerates blobs past the grace floor and,
for each, probes the indexed hash-ref columns on the referencing tables; a blob with no live
reference is collected. A **maintained refcount column or `blob_ref` table is a measured
optimization**, earned only if the per-blob probes profile too expensive (the same
ship-the-simple-thing discipline as the storage projections). The grace floor is the safety
margin against an in-flight reference, so GC never races a just-written event.

:::caution[Open question]
The grace-floor duration relative to the backtest window (long enough that a prospective backtest
re-deriving over the window cannot reference a collected blob).
:::

## Storage

The handle and the content-addressed bytes; the physical layout (the gateway, GC) is above and on [storage](/architecture/storage/).

| Table | Key columns | Notes |
|---|---|---|
| `file` | id, name, content_type, size, **sha256**, sensitive, (later tags) | **Partial.** Searchable metadata handle; points at a blob by hash. Tenant-wide (no placement arc); `sensitive` lifts the row to the `:admin` tier (defaults false). Tags-on-files is [#191](https://github.com/hyperscaleav/omniglass/issues/191) |
| `blob` | **sha256**, bytes / storage_ref, size | **Partial.** Content-addressed bytes; dedup on the hash. Default pgblobs (inline `bytea`); S3 / disk behind the same `blob.Store` seam. Content type lives on the file, not here (content-addressing is about the bytes). A file delete frees its unreferenced blob synchronously (dedup-aware); async mark-sweep GC of aged/event-referenced blobs is deferred |

