---
title: "A steady sim fleet no longer reads loss while the node and the sim disagree on it (#1962)"
date: 2026-11-23
refs: [1962, 1895]
---

The node rebuilds its worklist on its own clock and the simulator re-pulls its device list on
another, so a window with no change to the sim's served fleet could still count polls sent to a
device the sim had not bound yet (or had stopped serving) as SNMP loss. The sim's observation now
names the devices it serves (`served_ids`, capped at 2000, `served_truncated` past that), and
`Reconcile` moves the sends and connects a window could have drawn from unserved poll targets out of
the loss count into the masked term. A real loss on a served target still reads loss.

Also: a device that fails to start the same way on every pull no longer marks the fleet as changing
during each sync, so it does not make every bracket read `restarted`. The platform health guide now
says how long `restarted` lasts after a real fleet change. Tests: `TestReconcileSendsToUnservedTargetsAreNotLoss`,
`TestEngineFleetStaysSteadyDuringARetryOfADeviceThatKeepsFailing`.
