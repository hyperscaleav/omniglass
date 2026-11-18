---
title: "Place retires as a noun; location everywhere (#2039)"
date: 2026-11-16
refs: [2039, 2030, 2028]
---

ADR-2030 made location the one word for where things sit, and this sweep carries it through every surface. The console now says Parent location, Configure location and Start locations, and the detail view's place card is the location card (`LocationCard`). The wire names went with the words, since there is no installed base to keep: the `place_template` kind is `location_template`, a template body's `place` and `places` keys are `location` and `locations`, the alarm rule and notification fact `place` is `location` (a rule reads `location == "Boardroom"`), and the configure deep link `?edit=place` is `?edit=location`. The `find-and-fix-a-place` walkthrough is `find-and-fix-a-location`, and its docs shot `fleet-place-systems` is `fleet-location-systems`. The docs denylist bans the noun with an Allow for the English that keeps the word (in place, in its place, the one place, earns its place, decimal places, and the verb), and placed and placement never match. A new `BannedTerm.Matches` gives the simulator's catalog test the same Allow the docs lint applies.
