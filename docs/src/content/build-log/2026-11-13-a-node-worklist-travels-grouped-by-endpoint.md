---
title: "A node's worklist travels grouped by endpoint (#1583)"
date: 2026-11-13
refs: [1583, 1577, 1872, 1623]
---

The worklist reply now carries each endpoint's fields once, with its tasks under it, and the task
specs, session blocks and credentials those tasks share once for the whole reply, in dictionaries a
task names by index. The node reads back the same tasks, so nothing past the decode changes. On the
corporate plan with every https pin learned, the headquarters reply at 3,000 systems went from
6,875,172 B raw and 1,172,107 B gzipped (over the bus's 1 MB limit, as the soak found) to
1,508,216 B raw and 463,378 B gzipped; at 1,000 systems, from 575,058 B to 233,385 B gzipped. That
is under the 512 KB line ADR-1577 set for paging, so paging is not built. The server also
fingerprints each endpoint once rather than once per task. ADR-1583 records the shape.
