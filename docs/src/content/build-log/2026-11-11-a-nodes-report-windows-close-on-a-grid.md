---
title: "A node's report windows close on a grid (#1779)"
date: 2026-11-11
refs: [1779, 1751, 1625, 1369]
---

A node's collection report and its account of itself started each window at the moment the last one
was taken. The housekeeping pass that takes them runs every two seconds in the lab, so each window ran
up to two seconds past its length and the next began there: windows of 60 to 63 seconds, drifting off
the minute. A five-minute perf window over a 1,000-system lab held four or five reports per node,
which read as a 2.6% spread in metric rows per minute and refused a baseline taken at that size.

Both reports now close on one grid. Each window ends a whole window after the last, at an offset into
the window that the node's name sets, so a late take never lengthens the window that follows, a
restart keeps the node's offset, and a fleet's nodes land their reports spread across the window
rather than together. A take after a stall resumes at the next slot rather than catching up with
empty windows. Rows are still stamped when they are taken. The offset comes from the same name hash
that already phases the scheduler's tasks.

Unit tests drive a report through eight windows taken up to 1.999 seconds late and assert each is
due at exactly its grid end and not a nanosecond before, for both reports; a take after a stall is
due at the next slot; two names close at different offsets and a restart keeps the offset.
