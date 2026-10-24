---
title: "The fingerprint review's fixes (#1056, #1060)"
date: 2026-10-23
refs: [1056, 1060]
---

Each with a test that failed first. The gate (`endpoint-rejection`) republishes every pull until the
worklist reflects it, under a task chosen from the current worklist, so a lost publish or a
re-derived driver can no longer leave a restarted node without it. A refusal reported for an attempt
under an older fingerprint gates nothing. A host key trusted on first use stays out of the
fingerprint (only the declared pin goes in), so first use no longer recycles a healthy session. The
push lane refuses the node's control facts (`endpoint-rejection`, `ssh-host-key`). A request rung
refuses at write anywhere but a line over tcp (#821, #1037). The informational ping runs beside the
probe on a 500ms bound. Fixed-port simulator collisions are #1066.
