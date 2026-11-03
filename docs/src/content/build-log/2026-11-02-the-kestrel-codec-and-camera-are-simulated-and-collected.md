---
title: The Kestrel codec and camera are simulated and collected
date: 2026-11-02
refs: [897, 1266]
---

The Kestrel Relay C1 codec and Gaze P2 PTZ camera now have simulator profiles and drivers of
their own, so the dev seed's six placed Gaze cameras report their class from the camera's own API
instead of identity and `uptime` alone.

- **Relay C1** (`kestrel-relay-api`): call state, participant count, content sharing state and the
  count of connected peripherals. The profile serves dial, disconnect, share and stop, and a reboot.
- **Gaze P2** (`kestrel-gaze-api`): pan, tilt, magnification (the `zoom-level` metric), the recalled
  preset and the tracking mode. A preset recall or a position set moves the camera and puts it in
  manual tracking; while it tracks, a behavior drifts pan and tilt inside the mount's limits.
- **Neither is a video bar.** Each has its own profile and its own driver, and `kestrel-api` stays
  the VRoom's. Nothing is shared with `kestrel-vroom`'s profile.
- **New seed signals:** metric types `pan-angle`, `tilt-angle`, `zoom-level`, `camera-preset` and
  `peripheral-count`; property types `content-sharing-state` and `tracking-state`.
- **The field is `magnification`** in the camera's API, because the catalog lint refuses real company
  names and the lab's fictional vendor should not borrow one in its payloads.
- **The ratchet shrinks.** Both products leave the known-gap list in `coverage.yaml`.

No Relay C1 is placed in the dev fleet yet (#1446), so the Gaze cameras are the live check; the Relay
is covered by the profile test and the node test that runs both drivers against both profiles.
The class's full standard signal set is #1360.
