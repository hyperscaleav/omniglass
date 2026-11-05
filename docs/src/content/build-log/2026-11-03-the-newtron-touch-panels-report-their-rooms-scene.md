---
title: "The Newtron touch panels report their room's scene (#1494)"
date: 2026-11-03
refs: [1494, 1266, 1360, 1371]
---

The lab's Newtron touch panels reported only `video-input`, below the class-signal floor ruled on #1266.
They now report a second signal of their class: the room's scene (idle, presentation or video
conference), as the new `scene-state` property in the boot seed's property catalog.

The `newtron-nvp` driver reads it on a 30-second poll (`GET SCENE`), so a panel carries a value from its
first poll and not only after a scene change. The scene pushes (`EVT SCENE`) still land as raw log lines,
the unclaimed-push example the lanes test holds. The simulator's shared
`nvp-room` surface gains `GET SCENE` and `SET SCENE`, so the Core 3 processor, which includes the same
surface and reuses the panel's poll and listener blocks by anchor, reports it too, with no copy.

The lab's own integration test states the floor for the panels: after the warm-up, every placed Panel 7
carries a collected `scene-state` from an endpoint of its own. It stands until #1371 holds the floor for
every class. The panel's full class set (events and log lines per class) stays in #1360.
