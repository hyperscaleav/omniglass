---
title: Running in production
description: "What a production Omniglass needs: BYO PostgreSQL sized for telemetry, the three chart values, upgrades and rollback, the encryption key, and what is still to come."
sidebar:
  order: 4
  badge:
    text: Partial
    variant: note
---

A production deployment is the same binary and [chart](/deployment/helm/) as a preview, with three
values changed: a database you own, no auto-created owner, and a key you keep. The server runs as one
replica, the supported shape; [scaling and deployment](/architecture/scaling/) holds the deferred
fork for more. Once it is up, mint the first owner with `omniglass bootstrap`
([the CLI guide](/developer/cli/#authentication)), then move to the [Admin](/admin/) section.

## The three values

```yaml
postgres:
  enabled: false
externalDsn: "postgres://user:pass@managed-db.example.com:5432/omniglass?sslmode=require"
bootstrap:
  enabled: false
```

The bundled Postgres is an `emptyDir` and does not survive a pod restart, so it is for previews only.
`externalDsn` carries credentials: the chart makes a Secret from it and injects `OMNIGLASS_DSN` into the
server and the `migrate` init container. Supply it with `--set-file externalDsn=<file>` or fill it from
your secret tooling before install.

:::design[Target design, tracked in #1291]
The bundled Postgres keeps its data and says it is for demos only.
:::

## What to back up

Two things hold the state, and a restore needs both:

- **The PostgreSQL database.** Take your database's own backups; the chart never touches an external one.
- **The encryption key.** Every stored secret is sealed under it, and losing it makes them unreadable
  with no recovery. Read it out after the first install and keep it outside the cluster
  ([the encryption key](/deployment/helm/#the-encryption-key)).

The server's data volume holds the embedded bus's store, so a batch acknowledged before a restart lands
after it ([durable state](/deployment/helm/#the-data-volume)). It is not the system of record.

## Upgrades and rollback

Every rollout is gated on the schema. The `migrate` init container applies pending migrations under a
Postgres advisory lock, so replicas take turns, and it refuses a database a newer release migrated, so a
pod fails there rather than serve on a schema it does not know ([how it boots](/deployment/helm/#how-it-boots)).

Migrations only move forward. Back up the database before an upgrade; a `helm rollback` to an image older
than the schema stops at `migrate`, so roll back the image only together with that backup, or roll forward.

:::design[Target design, tracked in #1290]
A readiness probe that reflects the bus and its consumer, so a rollout waits on more than the process being up.
:::

## Sizing PostgreSQL

Postgres ships with settings sized for a small machine it shares with other work, and a fleet's
telemetry is a steady stream of writes. Set these in `postgresql.conf` (or as `-c` flags on the
`postgres` command). The scale lab runs the same settings, its `shared_buffers` also held to what
its simulated fleet leaves of the host, so a figure it measures is one a sized production database
would see:

| setting | value | why |
|---|---|---|
| `shared_buffers` | a quarter of the host's memory, up to 8 GB | the stock 128 MB holds a fraction of the recent telemetry, so reads that should hit memory go to disk |
| `max_wal_size` | `8GB` | the stock 1 GB had a 3,000-system soak checkpointing on WAL volume four times as often as on its timer, with commits stalling behind each checkpoint; leave 8 GB of disk free for it beside the data |
| `checkpoint_timeout` | `15min` | with the larger WAL, spaces checkpoints out to one each 15 minutes |
| `wal_compression` | `lz4` | compresses the full-page images each checkpoint forces into the WAL, for a little CPU |
| `track_io_timing`, `track_wal_io_timing` | `on` | records time spent in reads and WAL writes, so `pg_stat_io` says when the disk is the bottleneck |

Leave `synchronous_commit` on. Turning it off trades durability for throughput: a crash loses the
last transactions the server already acknowledged, and an acknowledged sample that disappears is the
loss the platform exists to measure. `track_commit_timestamp=on` is needed only by
[`omniglass lab measure`](/developer/lab/#measuring-a-scale-lab), which reads ingest lag from commit
times.

In a container, `shared_buffers` is not held in `/dev/shm`: Postgres maps it as anonymous memory, so
count the buffers against the container's memory limit. `/dev/shm` holds the segments parallel
queries share, and Docker's default 64 MB can be too small for a large parallel hash join, which then
fails with `could not resize shared memory segment`; raise it with `docker run --shm-size` or
Compose's `shm_size`.

:::design[Target design, tracked in #1293]
Resource profiles and a connection pool cap, from measurement. Until then, size the database as above
and watch the server's own memory.
:::

## Hardening and TLS

:::design[Target design, tracked in #1294 and #1295]
An optional network policy and restricted pod security (#1294), and the server terminating TLS itself
and verifying a client certificate (#1295). Until then terminate TLS at your ingress or proxy.
:::

When the server runs behind a reverse proxy, name the proxy ranges in `OMNIGLASS_TRUSTED_PROXIES` (for
example through `server.extraEnv`) so forwarded client addresses are honored.

:::design[Target design, tracked in #1231]
The trusted-proxy default is loopback only, and the chart declares its ingress range.
:::
