---
title: "The wire goldens and parity scaffolding are trimmed to a replay seam (#2837)"
date: 2026-11-28
refs: [2837, 2826]
---

The golden wire transcripts existed to prove each Go family matched the YAML profile it replaced, byte for
byte. With no YAML left that proof is moot, so it is trimmed. The 27 recorded transcripts under
`sim/testdata/wire`, the recorder (`TestRecordWire` and what only it used), the `Frozen`, `Quirk` and
`Diverges` hooks, the per-family parity and wire-script tests and the line, shell and event helpers that
served them are deleted. ADR-2832 to ADR-2836 still record what diverged from the profiles.

What stays is the replay half: the transcript format and `replayWire` in `sim/wire_replay_test.go`, the seam a
capture from a real device plugs into, with one small test (`sim/wire_replay_seam_test.go`) that replays a
transcript against `borealedge` and fails on an altered expectation. A family's own behavior is now held by
its own tests and by the driver contract test (`TestEveryShippedDriverPollResolvesAgainstItsSimulatedFamily`,
`internal/collection`): for every seeded product a family serves, the device starts on loopback, its shipped
driver's polls run through the real collection path, and every metric and property the driver declares must
resolve in every record, generated from the seed and the catalog (25 products, 23 drivers, 60 polls and 188
signals today). The tests of the families against `sim/behave` stay (`sim/behave_family_test.go`), and so do
the line-session tests that lived beside the recorder helpers (`sim/line_session_test.go`).
