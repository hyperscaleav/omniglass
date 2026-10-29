## fixture-name-the-seed-later-ships (open; found building #894)

- **Cue:** a test runs the real boot seed and then uses a plausible signal name as the one the
  catalog does not know (a push it expects refused, a 404 it expects, a custom type it creates by
  that name), while a diff adds rows under `internal/seed/*.yaml`.
- **Failure:** the seed later ships that name, and the fixture turns into a registered name with
  nothing on the diff pointing at it: the refused push is accepted, the custom create is a 409, and
  the test fails red far from the slice that caused it (`TestTelemetryPushAPI` with `audio-level`
  in #894, the metric-type CRUD tests with `fan-speed` in #898).
- **Fix:** name a must-be-unknown fixture with a reserved, self-describing non-name
  (`not-a-registered-metric`); when adding a seed name, grep `internal/` and `web/` for it first.
