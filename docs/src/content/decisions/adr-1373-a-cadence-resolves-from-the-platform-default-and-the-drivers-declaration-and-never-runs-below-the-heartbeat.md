---
title: "A cadence resolves from the platform default and the driver's declaration, and never runs below the heartbeat"
date: 2026-10-29
status: "Accepted"
summary: "Collection cadences resolve through one model: a driver poll's declared schedule.every, else collection.poll_seconds; a reachability probe at a node's own --probe-every, else collection.probe_seconds. Both platform defaults are the product's 30 s heartbeat and are refused below it, and so is a node's --probe-every below its own --heartbeat. A poll may now declare no schedule. Operator overrides down the cascade wait for #1351's node leaf and will be a dedicated typed binding, not a variable (#1443)"
refs: [1373, 1443, 1351, 1265]
---

- **Date:** 2026-10-29 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/#how-often-the-cadence-model), [nodes](/architecture/nodes/), [drivers](/guides/admin/drivers/), [admin guide](/guides/admin/)
- **Decision:** (#1373, split by the architect on 2026-10-29.)
  1. **Two platform defaults in the `collection` namespace.** `probe_seconds` is how often a
     reachability probe runs. `poll_seconds` is how often a driver poll that declares no schedule
     runs. Both are platform domain, admin-written and client-visible, and both reach every node on
     its worklist, as `report_seconds` does.
  2. **Most specific wins.** For a poll, its declared `schedule.every` wins over `poll_seconds`. For a
     probe, the node's own `--probe-every` wins over `probe_seconds`. A worklist from an older server
     carries neither default, and the node then runs every heartbeat as before.
  3. **A poll may declare no schedule.** It bakes with none, and the node reads that as "the
     platform's". Before, every poll had to declare one, and a baked task with none ran every tick.
  4. **The floor is the heartbeat.** Both defaults are the product's 30 s heartbeat and are refused
     below it at write time. A node's `--probe-every` is refused below its own `--heartbeat`. A
     throwaway lab now passes `--probe-every` equal to its 2 s heartbeat outright: a node with no
     interval of its own takes the platform's 30 s.
  5. **Overrides are deferred, and their cell is ruled.** An operator's override per component,
     system, location, product or node lands with the cascade's node leaf (#1351), as #1443. It is a
     dedicated typed binding, not a variable: a cadence has bounds and units a free-form variable
     cannot enforce. Per-endpoint-type catalog defaults move to #1443 with it.
- **Context:** #1373 left two questions open: the override's cell, and whether to wait for #1351.
  The defaults and the floor need neither, and they are what an install tunes its daily cost with
  (#1265). An install that sets nothing collects exactly as before, since both defaults equal the
  heartbeat a deployment runs at.
- **Not chosen:**
  - **A floor at each node's own heartbeat for the platform settings.** The server does not know the
    heartbeat each node runs at; a node's heartbeat is its own flag. The product's heartbeat is the
    floor a deployment runs at, and a lab that checks in faster says so with its own flags.
  - **Moving `report_seconds` onto each transport's catalog entry now.** That is part of the
    per-endpoint-type defaults, deferred to #1443.
