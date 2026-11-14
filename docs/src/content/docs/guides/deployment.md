---
title: Deployment
description: "Standing an Omniglass platform up: the single binary, the container image, the Helm chart, and per-PR preview environments."
---

This is the how-to for **standing the platform up**, a different job from
[operating the fleet](/guides/operator/) or [administering it](/guides/admin/). Omniglass is a
single self-contained Go binary with the operator console compiled in and a BYO PostgreSQL
database, so a deployment is that one binary in one of its run modes (`server`, `node run`, `migrate`)
plus a database. How it scales and the deploy model behind these pages is
[scaling and deployment](/architecture/scaling/).

To try it before deploying anything, [the lab](/guides/lab/) brings up a working omniglass on
Docker with one command, with a simulated fleet to collect from; it is for learning, not for
production.

Pick the path that fits where you are running it:

- **[Install](/guides/install/)** downloads a prebuilt binary for your OS and architecture, the
  simplest way to run the server or drive it from the command line.
- **[Container image](/guides/container-image/)** is the published distroless image: what it
  contains, where it ships, and how to run it with the `migrate` then `server` flow.
- **[Deploying with Helm](/guides/helm/)** is the chart that serves both production (BYO Postgres)
  and disposable previews (bundled Postgres) from one set of values.
- **[PR preview environments](/guides/pr-previews/)** stand up a live, isolated copy of the console
  for any pull request, so a change is reviewed against a running system.

Once a server is up, the first owner is minted with the trusted direct-database lane
(`omniglass bootstrap`), covered in [the CLI guide](/guides/cli/#authentication); from there,
managing accounts and access moves to the [admin guide](/guides/admin/).

## Sizing PostgreSQL

Postgres ships with settings sized for a small machine it shares with other work, and a fleet's
telemetry is a steady stream of writes. Set these in `postgresql.conf` (or as `-c` flags on the
`postgres` command); the scale lab runs with the same set, so a figure it measures is one a sized
production database would see:

| setting | value | why |
|---|---|---|
| `shared_buffers` | a quarter of the host's memory, up to 8 GB | the stock 128 MB holds a fraction of the recent telemetry, so reads that should hit memory go to disk |
| `max_wal_size` | `8GB` | the stock 1 GB had a 3,000-system soak checkpointing on WAL volume four times as often as on its timer, with commits stalling behind each checkpoint |
| `checkpoint_timeout` | `15min` | with the larger WAL, spaces checkpoints out to one each 15 minutes |
| `wal_compression` | `lz4` | compresses the full-page images each checkpoint forces into the WAL, for a little CPU |
| `track_io_timing`, `track_wal_io_timing` | `on` | records time spent in reads and WAL writes, so `pg_stat_io` says when the disk is the bottleneck |

Leave `synchronous_commit` on. Turning it off trades durability for throughput: a crash loses the
last transactions the server already acknowledged, and an acknowledged sample that disappears is the
loss the platform exists to measure. `track_commit_timestamp=on` is needed only by
[`omniglass lab measure`](/guides/lab/#measuring-a-scale-lab), which reads ingest lag from commit
times.

In a container, `shared_buffers` is not held in `/dev/shm`: Postgres maps it as anonymous memory, so
count the buffers against the container's memory limit. `/dev/shm` holds the segments parallel
queries share, and Docker's default 64 MB can be too small for a large parallel hash join, which then
fails with `could not resize shared memory segment`; raise it with `docker run --shm-size` or
Compose's `shm_size`.
