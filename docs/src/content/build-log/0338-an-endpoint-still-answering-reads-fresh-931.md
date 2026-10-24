---
title: "An endpoint still answering reads fresh (#931)"
date: 2026-10-20
refs: [931]
---

The reachability verdict is stored only when it changes, so its timestamp is the last up/down
transition, and the console aged it as though it were the last sample: an endpoint answering every
probe read **stale** in the Endpoints panel, and "stale sample; the node is healthy" in a component's
Collection card, a few minutes after its last change. The console now ages an endpoint by when it was
last seen (the server's stamp, #933 below), in the pill, the Collection state, its "last seen" age and
the panel's "last checked". Pure tests pin an old transition with a fresh stamp reading responding and
collecting.
