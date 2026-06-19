---
title: Files and blobs
description: A searchable file handle over a content-addressed blob store, behind the Storage Gateway.
---

Leaf of the [architecture spine](/architecture/). The opaque-bytes layer that closes the data
model: a searchable **`file`** handle over a content-addressed **blob** store, behind the same
Storage Gateway as everything else.

## Two layers: the file handle and the blob

- **`file`** is **indexable metadata**: name, content-type, size, `sha256`, tags. The searchable
  handle an operator references and finds (a firmware image, a device config dump, a runbook doc,
  a screenshot, a packet capture). It owns no bytes; it points at a blob by hash.
- **the blob store** holds the **bytes**, **content-addressed by `sha256`**. The hash is the key,
  so identical bytes are one blob.

Splitting them means search and inventory operations (list, filter, tag) never touch bytes, and
the same blob can back many file handles.

## Content-addressing earns four properties

A blob is keyed by the hash of its bytes, not a UUID, which buys:

- **dedup**: identical bytes collapse to one blob (two operators uploading the same firmware, the
  same `raw` payload seen twice);
- **integrity**: the hash verifies the bytes on read, tamper-evident by construction;
- **immutability**: bytes cannot change without changing the key, like the append-only
  ground-truth logs;
- **replay-stability**: a replayed event referencing a hash still resolves, because the hash is
  stable across replay.

So **rows reference a hash, never inline bytes.** Inline `bytea` would kill the narrow-replayable
property and bloat the firehose row. Small structured values (a datapoint, its labels) stay inline
in the row's jsonb; **large or opaque payloads become a blob hash-ref**: a big `log_datapoint`
body, and especially a **`collection.failed` event's raw** when the
wire payload is large (a full SNMP walk, a big HTTP body, a capture). Raw stays inline when small;
the size threshold is the switch.

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
reference is collected. A **maintained refcount column or `blob_ref` table is a deferred
optimization**, added only if the per-blob probes profile too expensive (the same
ship-the-simple-thing discipline as the storage projections). The grace floor is the safety
margin against an in-flight reference, so GC never races a just-written event.

## Open items

- The inline-versus-blob **size threshold** (one global cutoff, or per-kind: `raw` versus log
  body versus operator upload).
- The grace-floor duration relative to the replay window (long enough that a prospective replay
  re-deriving over the window cannot reference a collected blob).
- Whether `file` tags reuse the `tag` registry and cascade or are a flat per-file set.
- Chunking and streaming for very large blobs (firmware images, captures) on the `pgblobs`
  backend before S3 is configured.
