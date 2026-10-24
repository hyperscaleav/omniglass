---
title: "A command settles when its evidence arrives (#900)"
date: 2026-10-17
refs: [900]
---

Run against the simulator, `set-input` to a panel actuated and the device switched, yet the command
stayed `issued` and was later recorded `timed-out`: the panel is polled every 30 seconds against a
15-second window, settlement was checked only at issue, and the CLI's instance-less command never
met the endpoint-instanced observation. Settlement now follows the evidence (ADR-0141). Each driver
command binding declares its source: the reply (`from: response`, with a value `extract` recorded as
an observation, or a `success` confirmation) or the next observation (`from: observe`, the default).
The node carries the device's reply on its execution report and the server judges it by the
binding, recording a property target's value as a validated, transition-only observation; a matching observation taken after the intent settles a command inside its window; the
check runs when an observation lands; and a server sweep times out what nothing settles. A command
with no instance is judged on its dispatched endpoint's series. `newtron-nvp`'s `set-input` settles
from its reply, and the simulator end-to-end test now asserts the command settles.
