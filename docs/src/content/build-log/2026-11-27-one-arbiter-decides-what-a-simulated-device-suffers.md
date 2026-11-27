---
title: "One impairment arbiter decides what a simulated device suffers (#2799)"
date: 2026-11-27
refs: [2757, 2799]
---

The operator's impairment route and the scripted problems of the lab calendar no longer share a device by
luck. Each registers its wants with one arbiter per simulator engine under its own name, the arbiter resolves
them the same way every time (unplugged if any asks; the highest loss, latency and jitter; the newest pin on a
path, the older kept and restored in order) and is the only writer to the behavior-override set and the
impairer, which a guard test enforces. Clearing a scripted problem leaves an operator's unplug, and the
operator's repair leaves a problem's window. The meeting platform's offline listing reads the arbiter, so an
operator's unplug reads offline there as a scripted one does. Scenes (#2314) will be one more source.
