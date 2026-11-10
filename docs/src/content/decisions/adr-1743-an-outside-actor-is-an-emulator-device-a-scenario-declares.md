---
title: "An outside actor is an emulator device a scenario declares"
date: 2026-11-10
status: "Accepted"
summary: "A pusher is an emulator device of a profile with a push face (a cadence and a body template rendered per system), not code in the simulator's run mode. A scenario declares its pushers as it declares its devices (corporate: one booking service a site over four systems; the dev fleet: one booking service on the HQ boardroom), and both lab kinds provision them through one path: the seed grants each a telemetry:push principal scoped to its systems, the lab mints its token per run and sets it, and the simulator beside the pusher's node runs it holding that token. Supersedes the simnode booking-service goroutine and its single OMNIGLASS_SIM_PUSH_TOKEN of ADR-1370's third point"
refs: [1743, 1370, 1696, 1623]
---

- **Date:** 2026-11-10 | **Status:** Accepted | **Pages:** [simulation](/architecture/simulation/#a-pusher-pushes-on-its-own-cadence), [lab](/guides/lab/#the-manifest-format)
- **Decision:** (#1743.)
  1. **The push face is an engine primitive.** A profile may declare `push: {every, body}`: the body is a template rendered for each system the device's grant names, over its state, id and clock, and posted through `POST /telemetry:push` with the device's bearer token on every tick. The face reads its systems off its own grant (`GET /auth/me`) and reads them again after any failure. The booking service is a profile (`booking-service`), its meeting day profile state and `bookingsAt` a template function, so the next pusher is YAML.
  2. **A scenario declares its pushers like its devices.** A scale manifest's `pushers` names each one's profile, how many a site has and how many of the site's systems each pushes for; the validator refuses a profile with no push face and a pusher with no systems. The dev fleet's fixtures declare theirs (`sim_pushers`) in the same terms, with the systems named.
  3. **One provisioning path.** The lab plans every declared pusher on the node of its site (the dev lab's on its one node), mints its token per run, and hands the plaintext to that node's simulator alone (`OMNIGLASS_SIM_PUSHERS`) and the hash and prefix to the seed (`OMNIGLASS_LAB_PUSHER_TOKENS`). The seed grants each pusher's service principal the `telemetry-pusher` role (ADR-1370's grant shape) on each of its systems and makes the minted token its only one; the scale seed refuses a plan pusher with no token before it touches the database. The plan and the logs never carry a token.
- **Context:** ADR-1370's booking service was a goroutine in the simulator's run mode with one token in its own environment variable, so only the dev lab pushed and the scale lab's push lane read nothing unless an operator pushed by hand. #1696 gave the push lane a ledger, and #1623's measured lab needs every lab to exercise it.
- **Not chosen:**
  - **Keep the goroutine and add a scale twin.** Two code paths for one actor, and a new pusher would be Go, not a profile.
  - **A pusher as a component the node polls.** A push has no endpoint and nothing polls the actor; modelling it as one would invent a reachability it does not have.
  - **The seed mints the tokens.** The token would cross the seed's output to reach the simulator; minting in the lab hands each side only what it needs.
