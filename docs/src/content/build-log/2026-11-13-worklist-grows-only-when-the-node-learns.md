---
title: "A node's worklist grows only when the node learns (#1872)"
date: 2026-11-13
refs: [1872, 1583, 1623]
---

On the 3,000-system corporate soak, the headquarters node's compressed worklist went from 938 KB
to 1,171 KB with no operator change. The whole growth was the https key pins the node trusted on
first use: 440 endpoints in the first minute after boot. Each pin rides every one of its endpoint's
4,831 tasks at 71 bytes a task, exactly the 343,001 raw bytes the reply gained. The size then held
for the rest of the soak. So the worklist does not drift with time. The nodes page now names the
two learned fields, the key pin and the last rejection. A storage test holds two builds of an
unchanged worklist to the same bytes, before and after a pin is learned. The fix for the repeated
pin is the endpoint-grouped wire, #1583.
