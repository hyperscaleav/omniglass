---
title: "A command settles when its evidence arrives, from its reply first and observation second"
date: 2026-10-18
status: "Accepted"
summary: "A command settles as soon as its evidence arrives: a matching observation taken after the intent, or the device's reply when its driver binding says the reply carries it. Each binding declares its source (`from: response` with a value `extract` or a `success` match, or `from: observe`, the default); the window bounds the wait, not the settling; a sweep times out what nothing settles. Revises ADR-0063's pending-until-the-window-passes rule"
---

- **Date:** 2026-10-18 | **Status:** Accepted | **Pages:** [commands](/architecture/commands/)
- **Decision:** A settleable command settles the moment its evidence arrives, not when its window
  closes: a matching observation of its target taken at or after the intent settles it inside the
  window. Where the evidence comes from is declared on the driver's command binding:
  `settle: { from: response }` reads the device's reply to the command, with `extract` (the reply
  carries the new value, compared to the intent and, for a property target, recorded as a
  validated observation) or `success` (the reply only confirms); `from: observe`, the default, waits for the next observation. A reply that
  fails the binding's reading, or an execution error, fails a response-settled command at once.
  The settle check runs at issue, on the execution report, when an observation of the target lands,
  and on a server sweep that times out commands past their window. A command issued with no
  instance is judged on the series of the endpoint it was dispatched over. The early settle compares an
  observation's `ts` to the intent's: a polled observation carries the node's sample time, so a
  node clock behind the database's can push genuine evidence to the past-window judgment (late,
  never wrong); every other comparison keeps ADR-0108's one clock. **Amended (#456):** an
  un-instanced command not yet dispatched is judged on the endpoint queued to carry it, one no
  endpoint can carry on every live observed instance of its target, settled when any agrees, and a
  command no node has taken times out rather than failing on a reading
  ([ADR-0456](#adr-0456-an-instance-names-which-value-of-one-key-on-one-owner-a-row-is-and-an-owner-level-read-reduces-across-every-instance)).
- **Context:** ADR-0063 held a command pending for its whole window and judged it only after,
  and the check ran only at issue and on reads. Run against the simulator (#888, #900), `set-input`
  to a panel actuated, the device switched, and the next poll saw it, yet the command stayed
  `issued` and was later recorded `timed-out`: the panel is polled every 30 seconds against a
  15-second window, nothing re-checked a lone command, and the CLI's instance-less command never
  met the endpoint-instanced observation. The architect ruled that settlement reads the command's
  response first and the next observation second, configured per command binding. The window
  stays the driver's fact about actuation time; what changes is that it bounds the wait rather
  than delaying a verdict already in hand.
