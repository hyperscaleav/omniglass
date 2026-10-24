---
title: "A reconcile pass heals a rule decision lost to a race (#928)"
date: 2026-10-19
refs: [928]
---

The #920 review left two windows that could lose one decision on a transition-only input: a rule
enabled while a batch carrying its key was uncommitted (the prime cannot see the batch's sample, and the
batch loaded its rules before the enable), and a judgment aborted by a deadlock (the sample lands under
its savepoint, the action does not). Both now heal within a minute: the settlement tick runs a reconcile
pass that reads every series with a sample past the older of the last two passes' marks (a batch in
flight at one pass may commit ids below its mark), judges each enabled rule's state against the newest
sample in memory, and corrects only a series that drifted, in its own transaction under the documented
lock order. A pass over agreement writes nothing and keeps every streak's clock; a failed correction
rereads its range next time. Both windows are reproduced in testcontainer tests (the enable stalled
behind an in-flight batch, and a real deadlock the batch detects and aborts), each leaving drift on the
previous code and healed by one pass.
