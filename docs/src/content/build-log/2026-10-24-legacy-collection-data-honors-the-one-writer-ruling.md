---
title: "Legacy collection data honors the one-writer ruling (#1100)"
date: 2026-10-24
refs: [1100]
---

What was written or declared before the one-writer ruling is held to it wherever it would still act
([ADR-0163](/architecture/decisions/#adr-0163-legacy-rows-and-stored-declarations-honor-the-one-writer-ruling),
amending ADR-0161). A declared key pin (the ssh host key, the tls certificate's public key) is trusted
only as the endpoint's audited `:retry` wrote it: its row carries `source` `key-pin`, and the worklist,
the fingerprint, the reachability read and the audit row's replaced pin read a declared pin by that
mark alone. A node carries and republishes only a key it learned on first use itself, and forgets a
pin the worklist stops delivering, so its session redials trusting on first use. Two migrations: a
`retired_sample` table (DDL), and a one-time backfill that stamps the `:retry` pins written before the
mark (matched to the audit row their transaction wrote) and moves every row of a reserved key its class
would refuse today into `retired_sample`, whole, with its reason: a declared pin `:retry` did not
write and what a node published under it (ordered by arrival), a node-probe row on an owner that is
not a component, by a writer other than the node (a command's reply among them), under an instance
naming no endpoint of its component, or dated past the database clock, a `health` row the rollup did not write, and a `command-issued` event no command points
at. Retirement is a move, never a delete or a flag: no read serves a retired row, and its down restores
every one; a retired pin's key is never re-learned as the pin until a `:retry` declares it, and at boot
every event rule series a retirement touched is judged again, so a condition a retired row raised lets
go. The worklist strips from every driver task an emit the node lane may not carry, a
re-derivation never bakes one, and the server's boot audit names each stored driver spec, derived task,
contract line, event rule and command type the ruling refuses (a rule whose clear event the ruling now
reserves still lets its condition go). The node lane's writer binds the component its task collects for
as well as its endpoint (`NodeWriter(component, endpoint)`); contracts on all three classifiers refuse a
reserved key (a 422 naming it) and a legacy line lends no default; and `command-lifecycle` joins the
reserved family, holding `command-issued`, which only a command writes. Proven test-first, each test
shown failing before its fix: the gateway binds both halves of a node's writer
(`TestNodeWriterBindsItsComponentAndEndpoint`), a command's lifecycle event is refused to a push, a
node and an event rule (`TestCommandLifecycleIsTheCommandsAlone`), contracts refuse reserved keys on
the operator's and the seed's paths (`TestContractsRefuseAReservedKey`), a planted ssh or tls pin is
never delivered and moves no fingerprint while a `:retry` pin is (`TestPlantedPinIsNeverDelivered`), a
node forgets a withdrawn pin (`TestSSHWithdrawnPinIsForgottenAndTrustsOnFirstUseAgain`,
`TestTLSWithdrawnPinIsForgottenAndTrustsOnFirstUseAgain`), stored specs and tasks carry no refused emit
to a node (`TestStoredSpecsAndTasksCarryNoReservedEmit`), and the backfill retires exactly what the
ruling refuses, leaves the honest rows, changes nothing on a second run and is reversed by its down
(`TestLegacyBackfillRetiresWhatTheRulingRefuses`, `TestLegacyBackfillDownRestoresTheRows`), with its key
lists held to the code's (`TestLegacyBackfillNamesTheRuling`). The adversarial review's in-scope
findings were fixed the same way: a retired pin is never re-learned from the series
(`TestRetiredPlantedPinIsNeverRelearned`), a condition a retired row raised is released
(`TestRetiredRowsReleaseTheConditionsTheyRaised`), and a legacy rule still lets go
(`TestARuleWhoseEventBecameReservedCanStillLetGo`). The contract tests that used a node's
probe metric as a stand-in now use collected metrics, and the dev seed's breadth round-trip series lands
under each component's own endpoint once the simulated fleet gives it one.
