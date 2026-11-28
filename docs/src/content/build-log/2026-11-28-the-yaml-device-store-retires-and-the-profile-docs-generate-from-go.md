---
title: "The YAML device store retires, and the simulator's family and coverage tables generate from Go (#2837)"
date: 2026-11-28
refs: [2837, 2826, 2829]
---

With every simulated product a Go family, what only YAML profiles used is deleted. The profile store and its
loader, `schema.json`, the surface fragments, the response and behavior templates, the `sim/profiles` embed,
the `spec.FromProfile` adapter and `Kind`, `Spec.Behaviors`, the engine's template routes, commands and
behaviors, the push-body template, the subject push-format template and the `Native*` workaround fields
(the console's listing of a family is built from its own typed declarations: `wire.Route.Sets`,
`wire.Interface.OIDs`) are gone. `spec.NewCatalog` takes the families alone. A guard test in
`internal/build` fails the build if a YAML file, a template or a YAML decoder returns under `sim/`.

What stayed is what operators and families still use. A scenario's override expression is a template an
operator writes, so the expression engine (`now`, `get`, the numeric, scheduling and seeded-random helpers)
stays, cut to what an override can read. The types a family composes (interfaces, auth, subjects, session
limits, the route and command listing) left a `profile` package that was no longer a profile: they are
`sim/wire`.

The simulation page's family and model tables and its coverage tables were hand-written. `cmd/simgen` now
renders them, with each model's driver and component type from the seed, into
`docs/src/generated/simulation.json`, and `SimFamilies` and `SimCoverage` render that. `coverage.yaml`
became typed Go (`sim/device/coverage.go`), so the alignment and fleet-coverage tests, and the page, read one
accounting. A new family changes the page through `make gen`; a stale committed file fails
`internal/simfacts`.

The tests whose only subject was a YAML path went with it (the template loader and its checks, the mutation
and route-validation tables, the behavior, emit and computed template tests, the profile-to-family
parity check, the template helpers). The ones whose subject was a face (HTTP auth, the SSH, Telnet and
raw-TCP sessions, the event stream, observation, faults, operate) now stand on a Go route or command in
place of a template. ADR-2829 is amended: the strangler is over.

**Breaking: a simulated behavior no longer carries `emits`.** `SimBehaviorBody`, in the simulation routes'
behaviors list, lost its `emits` flag. It said a YAML behavior posted a webhook on its timer; a family posts
from its own handlers, so with no YAML left the flag was always false, and the console's "posts a webhook"
badge went with it. A client reading `emits` reads nothing now, which is what the field held. The webhooks
themselves are unchanged.
