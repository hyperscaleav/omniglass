---
title: "Volume follows the ratio canon, in and out (#1429)"
date: 2026-11-14
refs: [1429]
---

`volume` stored 0 to 100 under `unit: "%"`, so a rule written `volume > 0.9` (the docs' ratio canon) fired on every component not muted to zero. It is now a `ratio` with `precision: 3`, like `battery-charge` and `storage-utilization`, and the `kestrel-api` and `kestrel-ws` drivers read the bar's percentage with `transform: { scale: 0.01 }`. The settable path follows: a command binding may declare a `transform` with a `scale` (validated like an emit's, refusing a cast or map), applied to `${value}` before substitution, so an intended 0.41 goes out as the typed number 41; a response settle reads the reply back through the same scale, so it compares against the intended ratio. A scaled value is held to 15 significant digits, so 41 reads as exactly 0.41 and the observed value equals the intended one. The `set-volume` command type now takes a fraction of the range.
