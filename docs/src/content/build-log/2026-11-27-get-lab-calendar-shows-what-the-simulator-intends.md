---
title: "GET /lab/calendar shows what the simulator intends (#2770)"
date: 2026-11-27
refs: [2757, 2770]
---

A lab server now serves the lab calendar: `GET /lab/calendar` with a bounded `start` and `end` (at most 31
days) and optional `site`, `building`, `room` and `system` filters, and the generated `omniglass lab calendar
list`. It is lab-only (404 elsewhere), gated by `simulation:read`, and limited to rooms whose system is in the
caller's system read scope. The server computes the calendar itself, from the world file the lab's seed wrote
into the shared data volume and the same functions the simulator runs, so it never asks a simulator or reads
collected data, and it needed no new setting and no migration. A lab test holds the route's entries equal to
what a camera in the room frames over a simulated day. The generated `lab` command group now merges into the
hand-written one instead of being shadowed by it.
