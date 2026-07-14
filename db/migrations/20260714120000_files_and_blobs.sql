-- migrate:up

-- blob: the content-addressed byte store. The primary key is the sha256 of the
-- bytes, so identical bytes collapse to one row (dedup), the hash verifies the
-- bytes on read (tamper-evident), and a stored blob is immutable. This is the
-- default pgblobs backend (bytes held inline in Postgres); an S3-compatible or
-- disk backend swaps behind the same blob.Store seam with no model change (it
-- would carry a storage_ref instead of inline bytes). Content type is not stored
-- here: content-addressing is about the bytes, so the declared type lives on the
-- file handle, not the blob. Additive and idempotent.
create table if not exists blob (
    sha256     text        primary key,
    bytes      bytea       not null,
    size       bigint      not null,
    created_at timestamptz not null default now()
);

-- migrate:down

drop table if exists blob;
