---
title: "A node reports how busy its worker pool was (#1686)"
date: 2026-11-13
refs: [1686, 1638, 1624]
---

A node now sends `worker-busy-fraction` in its node-self block once per report window: the share of
its lane pool's worker time spent running polls, probes, commands and runs, from 0 (idle) to 1
(every worker busy all window). The window's high-water marks could not say this. A peak of 4 busy
workers reads the same whether they were busy for a millisecond or the whole window. The busy
fraction is the headroom figure that spreading a site's components across its nodes will read
(#1638). The pool keeps the tally with the dispatcher's clock. A run still in flight at the window's
end counts the part it has run, and the rest lands in the next window. The node runs one pool for
every kind of discrete work, so the figure carries the instance `lane`. Building it showed that the
key must be seeded before a node sends it. The server refuses a batch that names an undeclared key,
and that refusal took the batch's other figures down with it.
