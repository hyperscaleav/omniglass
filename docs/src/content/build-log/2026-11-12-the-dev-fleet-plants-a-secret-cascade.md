---
title: "The dev fleet plants a secret cascade to read (#168)"
date: 2026-11-12
refs: [168, 155, 820]
---

A fresh `make dev` and the lab now come up with one secret name at three tiers: `sim-panel-login`
exists at the platform, on Boardroom A, and on the touch panel inside it. The panel's effective
secrets show its own copy as the winner with the room's and the platform's shadowed beneath it, a
sibling in the room resolves the room's copy, and every other component the platform's. There is no
system tier, because secrets have no system band.

A fixture secret (`sim_secrets` in `fixtures.yaml`) can now name an `owner_kind` (`location` or
`component`) and an `owner` (a fixture key, resolved to the seeded row's id like a component's
`location`); a secret naming neither stays a platform secret. They are created through the Storage
Gateway like the rest, and a re-run finds each tier's copy already there and creates none.

Two constraints shaped which name is planted and when. An attach refuses a secret name with more than
one copy to choose from (#820), so the copies below the platform are created after every endpoint. And
a node refuses a name with more than one copy cascading onto a component, so the planted name must be
one no endpoint beneath the planted owner references; `sim-panel-login` is used only by the huddle
panel's console, in another room. A fixture test holds that rule for any later planted secret.

An integration test on a real Postgres runs the dev seed and resolves the effective secrets of the
panel (component winner, room and platform shadowed), of a video bar in the room (room winner,
platform shadowed) and of a huddle display (platform alone), and a second seed run leaves the secret
count where it was.
