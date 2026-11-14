---
title: "A node sizes its lane pool from its CPU and memory"
date: 2026-11-13
status: "Accepted"
summary: "`--lane-workers` defaults to `auto` rather than 16 (#1869): the node reads its cgroup's `cpu.max` and `memory.max`, the tightest up the tree, each held to the host's CPUs and memory, and takes the tighter of half the CPUs times a run's whole time over its CPU time (about 23 lanes a CPU at 4.4 ms of CPU and 200 ms of wait) and a quarter of the memory at 1 MiB a lane, clamped to 4 to 256. A number still sets the pool outright. The node logs the count and its source at boot and reports it as `lane-worker-count`. Amends ADR-1249"
refs: [1869, 1249, 1686, 1623]
---

- **Date:** 2026-11-13 | **Status:** Accepted | **Pages:** [nodes](/architecture/nodes/)
- **Decision:** `--lane-workers` (`OMNIGLASS_LANE_WORKERS`) takes a count or `auto`, and `auto` is the default. Auto reads the CPUs and memory the node may use from its cgroup v2 (`cpu.max`, `memory.max`, the tightest of its cgroup and every parent's), and takes each as the tighter of the cgroup's and the host's (the host's CPUs are `GOMAXPROCS` where it is below the machine's count, which carries a cgroup v1 CPU limit and an operator's own setting; its memory is `MemTotal`), so a limit the cgroup leaves open, or sets above the host, is the host's. The pool is the tighter of a CPU bound, `CPUs x 0.5 x (CPU + wait) / CPU` per run, and a memory bound, `memory x 0.25 / 1 MiB`, clamped to 4 to 256. The node logs the count it took and the limits it read at boot, and reports the count once per report window as `lane-worker-count` under the instance `lane`.
- **Context:** [ADR-1249](#adr-1249-the-endpoint-lane-runs-one-request-at-a-time-operator-work-first) fixed the default at 16, which leaves a large host idle and can oversubscribe a container on a fraction of a CPU. The CPU count alone is no better: a lane spends most of a run waiting on its device, so a CPU keeps many runs in flight, and the ratio of a run's whole time to its CPU time sets how many. The cost figures are the product's measured ones, conservative (the 3,000-endpoint lab's busiest node, simulator included, ran 4.4 ms of CPU a poll); a node whose drivers cost more sets a count. Reporting the count beside `worker-busy-fraction` lets an operator read a saturated pool against its size.
- **Not chosen:**
  - **The cgroup's figure as read.** A quota above the host's capacity would size the pool for CPUs and memory the node cannot have, so each limit is held to the host's.
  - **Resizing the pool at runtime from its busy fraction.** The count is fixed at boot; `worker-busy-fraction` (#1686) is reported for an operator to read against it, not fed back.
  - **Reading cgroup v1's memory limit.** A v1 CPU limit reaches the node through `GOMAXPROCS`; a v1 memory limit does not, and the node sizes from the host's `MemTotal` there. cgroup v2 is the default on every current distribution and container runtime.
- **Amends:** [ADR-1249](#adr-1249-the-endpoint-lane-runs-one-request-at-a-time-operator-work-first) (the `--lane-workers` default of 16, now `auto`).
