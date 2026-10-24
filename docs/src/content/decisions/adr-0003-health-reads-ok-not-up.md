---
title: "Health reads `ok`, not `up`"
date: 2026-06-27
status: "Superseded by [ADR-0050](#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain)"
summary: "The healthy state is named `ok`, not `up`"
---

- **Date:** 2026-06-27 | **Status:** Accepted | **Pages:** [health](/architecture/health/)
- **Decision:** The healthy state of a component or system is named **`ok`**. An earlier draft used `up`.
- **Context:** `up` reads as reachability (the device answers), which is only one input to health. Health is
  a rollup verdict ("is this system working?") that can be unhealthy while every device is reachable, or
  healthy while a redundant member is down. `ok` names the verdict rather than the ping, so the word does
  not promise something narrower than the model delivers.
- **Superseded by** [ADR-0050](#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain)
  on the **word only**. The reasoning holds and the built domain still names the verdict rather than the ping;
  it spells the members **`healthy` / `degraded` / `outage`**, because `outage` says what a broken system means
  to the people in the room where `down` says what a device is doing.
