---
title: "The Boreal media player is simulated and collected (#893)"
date: 2026-10-29
refs: [893, 1266]
---

The dev fleet's four Boreal Cast M1 signage players had no simulator profile, so the lab placed them
and collected nothing from them. They now have one, and a driver of their own. The catalog had put
them on `http-json`, but that driver is the Tessellate sensor's: it reads occupancy behind the
sensor's own header, and nothing a media player reports.

The profile is one face, the way signage players are integrated with a content system: an HTTP JSON
API behind an api key in the `X-Boreal-Key` header. Its status reports identity, uptime and how full
the player's storage is; its playback route reports whether it is playing and which item, and sets
play, pause or stop. While it plays, a behavior steps through its four-item playlist, wrapping at the
end; paused or stopped, the item holds. Storage drifts slowly between bounds. A scenario timeline can
stop a sign or fill its disk later.

The new `boreal-cast-api` driver reads identity and `storage-used` every five minutes and
`playback-state` and `content-name` every 30 seconds, three signals new to the boot seed's canon. The
lab seeds the player's key as a platform secret beside the other simulated credentials, and attaches
the driver with it; the player leaves `coverage.yaml`. The simulation page now states the attach rule
instead of listing each driver.
