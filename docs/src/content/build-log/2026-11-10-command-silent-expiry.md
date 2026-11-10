---
title: "A fire-and-forget command no node carries out times out instead of reading settled (#1630)"
date: 2026-11-10
refs: [1630, 1155, 1675]
---

A command with nothing to settle, such as `reboot`, used to be inserted `settled` at its issue, so a
reboot no node ever took, or one the device refused, read as done. It is now issued `issued` like any
other command and its node's execution report decides it: `settled` by `reply` when the node carried
it out, `failed` by `reply` with the masked error when the execution failed, and `failed` at once when
its binding cannot render. With no report inside its ten minute delivery window the settlement sweep
records `timed-out` by `window`, its reason naming where it stopped (queued for a node that did not
take it, delivered with no report, queued on an endpoint placed on no node, or no endpoint whose
driver binds it), and its lifecycle event lands as every other terminal status's does. The read's
`deadline` for such a command is the end of that window and its phase reads `queued`, then
`delivered`. The pending query now delivers only a command still `issued`, so a command the console
already reads as timed out is never actuated afterwards (#1675); one whose settle window passes before its node's next pull now times out unsent, until faster pickup (#1635). Both sides of the command pull now log a
failure at Warn with `facility=command` and the node's name; the node logs only the transition into
failing and an Info line on recovery, so a down server costs two lines, not one a heartbeat
([ADR-1630](/architecture/decisions/#adr-1630-a-fire-and-forget-command-settles-on-its-report)).
