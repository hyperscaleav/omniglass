---
title: "The scale lab runs Postgres sized for the fleet it simulates (#1930)"
date: 2026-11-14
refs: [1930, 1633]
---

The scale lab ran Postgres on its stock settings, and the 3,000-system soak checkpointed on WAL volume
four times as often as on its timer, a commit stalling behind each checkpoint. A scale lab's database
now starts with `shared_buffers` at a quarter of the Docker host's memory (128 MB to 8 GB, 1 GB when
the host is not read, and the forced `--host-memory` when one is given), `max_wal_size=8GB`,
`checkpoint_timeout=15min`, `wal_compression=lz4` and I/O timing on, beside the
`track_commit_timestamp` that `lab measure` already needed. `synchronous_commit` stays on. The runner
reads the host before it plans, so the plan stays pure. A `dev` lab still runs stock Postgres. Checked
inside `postgres:18`: the buffers are anonymous memory, not `/dev/shm`, so the container needs no
`shm_size`. The deployment guide gains a sizing section naming each setting and why; unit tests pin
the command, the bounded quarter and the host read.
