---
title: "The Go device-family pattern repeats no workaround (#2845)"
date: 2026-11-27
refs: [2845, 2826]
---

A slice of the Go-families epic (#2826), run before the four remaining family conversions so they copy
none of the pilot's workarounds. Uptime is a typed `spec.Elapsed` a family declares, not the YAML
`Computed` seam, and the Boreal transcripts replay unchanged. The golden wire recorder and replay are one
generic harness: a family supplies a `wireFamily` (products, script, SNMP reads, drifting behaviors, room
day), records it with one command and replays it with one call, and a test-only family proves the harness
with no Boreal code involved. Tests build their engine over the shipped catalog through
`sim/simtest/shipped`, so converting a profile to a family touches no test, and a guard in
`internal/build` refuses a new engine built from the YAML store. `httpapi.Control` refuses a body that is
not a JSON object with a 400 and writes nothing. The policies (quirk steps last, the unreadable body, the
catalog-guard pin for tests that name shipped products, how to record a family) are written once in
`sim/README.md`.
