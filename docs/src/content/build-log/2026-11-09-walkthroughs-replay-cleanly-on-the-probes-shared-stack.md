---
title: "Walkthroughs replay cleanly on the probe's shared stack (#1404)"
date: 2026-11-09
refs: [1404, 1459]
---

The probe replays every walkthrough in alphabetical order on one lab, while the recorder gives each a
lab of its own, and a setup's "already exists counts as made" rule hands one walkthrough the state
another left behind. Two such collisions are fixed at the walkthrough. snooze-an-alarm clicked a bare
"Snooze" while preview-a-notification and subscribe-to-alarms had alarms on the same panel, so two
rows carried one and the locator was a strict-mode violation; its alarm now has a message of its own
and its steps name the button for it. The leaf's Snooze and Unsnooze buttons, which every alarm row
renders, now take an accessible name that ends in the alarm's message ("Snooze Touch controller
resetting"), so two of them are no longer both plainly "Snooze" to a screen reader either.
set-up-monitoring attached SNMP to `bay-2-display`, which fix-an-endpoint-in-place had already given an
SNMP endpoint, and was refused with a 409; it now sets up `bay-2-confidence-display` of its own.

The next collision fails at validation: `setupCollisions` in `walkthroughlib.mjs` flags two
walkthroughs whose setups create the same component or raise the same alarm (component and dedup key)
when any of them acts, and `make test-web` runs it over the shipped set. The setup-isolation rule is
in the demos guide, and the synthetic-user guide says the probe shares one stack.
