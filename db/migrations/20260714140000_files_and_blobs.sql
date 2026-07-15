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

-- file: the searchable metadata handle over a blob. It owns no bytes; sha256
-- references a blob by its content hash (many file handles can share one blob,
-- the dedup payoff). A file carries no placement on the estate arc: it is
-- tenant-wide, gated by the file:<action> permission plus the sensitive flag
-- (below), not by ABAC tree scope. sensitive mirrors secret.admin_sensitive: a
-- flagged file is lifted to the :admin permission tier (invisible to a lister
-- without it, a non-disclosing 404 to a reader without it). It defaults false
-- (a file is shared unless marked), unlike a secret which defaults sensitive.
-- content_type is the declared type for serving; it lives here, not on the blob,
-- because content-addressing is about the bytes. Additive and idempotent.
create table if not exists file (
    id           uuid        primary key default uuidv7(),
    name         text        not null,
    content_type text        not null,
    size         bigint      not null,
    sha256       text        not null references blob (sha256),
    sensitive    boolean     not null default false,
    created_at   timestamptz not null default now(),
    updated_at   timestamptz not null default now()
);

-- The GC probe (a later slice) walks file.sha256 to find live blob references.
create index if not exists file_sha256 on file (sha256);

-- migrate:down

drop table if exists file;
drop table if exists blob;
