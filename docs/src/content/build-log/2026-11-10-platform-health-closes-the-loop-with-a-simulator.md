---
title: "Platform health closes the loop with a simulator connected (#1647)"
date: 2026-11-10
refs: [1647, 1639, 1644, 1662]
---

With a simulator connected, [Platform health](/guides/operator/platform-health/#with-a-simulator) gains
a section that sets what each simulated device saw beside what the node sent and the server collected.
Each node with a simulator gets a card: every hop a request or a batch travels (connect, send, settle,
return, publish, spool, and poll targets the simulator never saw) read as a loss ratio with its lost and
in counts and its masking term, then each protocol's accepted, received and answered counts, and each
simulated device's requests, its impaired flag, and whether it was due and never seen. The figures come
from a sliding window the server holds: `internal/closedloop`, the reconcile #1639 built for the scale
lab moved into a pure package, now also has a ring that brackets each node once a minute (every 10 seconds
on a lab, every second on a test lab) and reads its last ten brackets, so a loss reads while it is
current and clears once the window slides past it
([ADR-1647](/architecture/decisions/#adr-1647-platform-health-closes-the-loop-over-a-server-held-sliding-window)).
A gap on a node whose simulator carries an impairment reads impaired, not lost; a window holding a
restart, or not yet full, says so and shows no number. The read is `GET /views/platform/simulation`
(`omniglass view platform simulation list`), behind `simulation:read` and a fleet-wide component read.
A new walkthrough drops an snmp device's packets from its drawer and watches the gap open and clear, and
an end-to-end check holds a whole lab to every gap at 0 and every polled protocol reached. A simulated
device's drawer now opens from `/simulation?c=<component>`, and a zero impairment field starts empty, so
a typed value replaces it.
