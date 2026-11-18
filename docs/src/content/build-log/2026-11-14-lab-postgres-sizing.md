---
title: "The scale lab runs Postgres sized for the fleet it simulates (#1930)"
date: 2026-11-14
refs: [1930, 1633]
---

The scale lab ran Postgres on its stock settings, and the 3,000-system soak checkpointed on WAL volume
four times as often as on its timer, a commit stalling behind each checkpoint. A scale lab's database
now starts with `shared_buffers` at a quarter of the Docker host's memory (128 MB to 8 GB, 1 GB when
the host is not read), never more than the fleet's estimate leaves of what the host can spare, with
`max_wal_size=8GB`, `checkpoint_timeout=15min`, `wal_compression=lz4` and I/O timing on, beside the
`track_commit_timestamp` that `lab measure` already needed. `synchronous_commit` stays on. The
preflight counts buffers above the stock 128 MB on top of the estimate (the stock size is what the
estimate was measured with), so the buffers never refuse a fleet the estimate fits; the spare cap
keeps 3,000 systems fitting on a 12 GB host, and a forced `--host-memory` sizes the buffers only when it is less than the host has.
The runner plans, reads the host, then plans again with the buffers, so the plan stays pure and a
bad flag is refused before the host is read. A `dev` lab still runs stock Postgres. The deployment
guide gains a sizing section naming each setting and why, and says when a container's `/dev/shm`
needs raising; unit tests pin the command, the bounded quarter, the spare cap, the floor and the host read.
