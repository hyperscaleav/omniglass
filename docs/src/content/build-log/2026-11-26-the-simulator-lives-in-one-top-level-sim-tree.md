---
title: "The simulator lives in one top-level sim/ tree (#2758)"
date: 2026-11-26
refs: [2758]
---

The simulator was five sibling packages under `internal/` (`sim`, `simfleet`, `simhub`,
`simoutpost`, `mailsim`). They now sit in one top-level `sim/` tree in the same Go module, with the
engine at `sim/`, its profiles at `sim/profiles/`, and the rest as `sim/simfleet`, `sim/simhub`,
`sim/simoutpost`, `sim/mailsim`, `sim/fleet`, `sim/impair`, `sim/profile` and `sim/simtest`. Every
one has an importer outside the simulator (server, outpost, lab, devseed or tests), so none moved
under `sim/internal/`. A move-only change: import paths, the Makefile and the docs' links changed,
no behavior did. `sim/README.md` says what the simulator is, how it runs and how a profile is written.
