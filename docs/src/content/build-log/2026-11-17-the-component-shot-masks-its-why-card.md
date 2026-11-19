---
title: "The component page shot masks its Why card whole (#2117)"
date: 2026-11-17
refs: [2117]
---

The `fleet-component` docs shot failed the freshness gate on branch after branch with the same
492 changed pixels. Its alarm row was masked whole, but the Acknowledge and Snooze buttons cast a
shadow a pixel or two below the row's own box, outside the mask, and those buttons follow the
alarm's age on the same line, so the live sliver moved sideways whenever the age changed width
("54s" against "1m"). The shot now masks the `leaf-alarms` section, a box the layout pins whose
padding holds the shadows. The old drift lies inside the new box, and a recapture at a fresh age
holds pixel-exact.
