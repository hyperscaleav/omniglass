---
title: "A reboot holds its device's endpoints until each reads up again (#1651)"
date: 2026-11-09
refs: [1651, 1650, 1624]
---

A disruptive command now closes its endpoints' gates instead of letting a rebooting device read as
loss ([ADR-1651](/architecture/decisions/#adr-1651-a-disruptive-command-closes-its-endpoints-gates)).
A command type gains `disruptive`, `quiet_seconds` and `hold_seconds`, and `reboot` ships disruptive
with a 15 s quiet and a 300 s hold. The node delivery carries the flag and the hold set (every
endpoint of the component on that node), and the node adds a `disrupted` reason to each gate before
it sends. During quiet nothing runs against them: due polls are held and counted as
`poll-held-count`, the probe is skipped, a held session waits to dial, and a command, run now or
preview answers at once naming the command. After quiet each endpoint reopens on its own first up
verdict from a probe begun after quiet, so a reply in flight at the delivery cannot release it; the
`hold_seconds` backstop reopens one that never answers and counts it as the new
`hold-expiry-count`. Proven over the real wire (`TestARebootHoldsItsDevicesEndpointsUntilEachReadsUpAgain`):
a reboot to a simulated sensor holds its http and tcp endpoints while the device is gone, keeps them
held past quiet while it stays gone, reopens each on its own up when it returns on the same
address, and lands no poll failure and no hold expiry across the outage.
