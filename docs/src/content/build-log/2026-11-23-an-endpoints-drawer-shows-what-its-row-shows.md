---
title: "An endpoint's drawer shows what its row shows, and more (#2436)"
date: 2026-11-23
refs: [2436, 2423, 2427, 1657]
---

The endpoint drawer showed less than the component page's own endpoint row: no availability
strip, no uptime, no parameters, inputs or out-of-band flag. An endpoint has no page, so the
drawer is the one place to read it whole. The expanded row and the drawer now render one
`EndpointDetail` body, with a parity test that mounts both hosts and compares their field ids
and a guard that fails if either host grows markup of its own.

The body adds the driver (linked through the drawer address) and version, each reach rung's
last result and age, held gate reasons beside the session, parameters and inputs with their
provenance, the bound secrets by name only, and each poll with its interval, last read and
mean duration beside the poll test. Long sections fold under a one-line summary.

Divergence: the server keeps no per-poll run record, so a poll's last read and run time come
from the component's effective metrics for this endpoint and its `poll-duration` series (a
report-window mean), not from a recorded run; the poll test shows the exact read. Hovering an
endpoint row now lights the whole row, not its left half.
