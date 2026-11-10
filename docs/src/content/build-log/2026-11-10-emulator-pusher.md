---
title: "Every lab pushes: the booking service is an emulator device a scenario declares (#1743)"
date: 2026-11-10
refs: [1743, 1696, 1370, 1623]
---

The simulated booking service moved out of the simulator's run mode into the sim engine as a
`booking-service` profile with a **push face**: a cadence and a body template rendered per system,
posted through `POST /telemetry:push` with the device's bearer token, its systems read off its own
grant. Scenarios now declare pushers like devices: `corporate` places one booking service a site over
four of its systems, and the dev fleet's single booking service is a `sim_pushers` declaration in the
same form. Both lab kinds provision them one way: the lab mints each pusher's token per run, the seed
grants its principal `telemetry:push` on its systems and sets the token, and the simulator beside the
pusher's node runs it. A corporate lab's `lab measure` now reads push-lane ingest rows at loss 0. The
simnode goroutine and `OMNIGLASS_SIM_PUSH_TOKEN` are gone. The decision is
[ADR-1743](/architecture/decisions/#adr-1743-an-outside-actor-is-an-emulator-device-a-scenario-declares).
