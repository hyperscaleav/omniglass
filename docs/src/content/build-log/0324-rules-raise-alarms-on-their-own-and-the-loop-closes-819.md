---
title: "Rules raise alarms on their own, and the loop closes (#819, slices #919 and #920)"
date: 2026-10-19
refs: [819, 919, 920]
---

The evaluator judges each landed sample against the enabled rules reading its key, in the sample's
transaction and under a savepoint (a rule's failure is logged, the sample lands), through the same
guarded raise and clear a hand raise uses. One alarm per component and instance, keyed `rule:<name>:<instance>`
so a rule's conditions never share a hand raise's namespace; a sample older than its series' newest
decides nothing, so a late or redelivered `down` cannot reopen an incident; disabling, deleting or
narrowing a rule clears what it holds. Sustains run on a durable streak per rule, component and instance,
acted on by a later sample or by a sweep on the settlement tick: the architect's ruling when it turned out
the reachability verdict reports only on change. The adversarial review hardened the streak: the sweep
and an arriving sample serialize on a per-series advisory lock, and the sweep claims a streak by
compare-and-delete, re-judges the newest sample and acts in a transaction per streak, so a recovery that
lands as the sustain runs out can no longer leave the alarm open; an ingest holds the rules it judges by
(FOR SHARE) for its batch, so a disable or delete waits for an in-flight batch and clears what it raised; sustains are timed by the server's
receipt, not the node's clock; only a material edit restarts a streak; and creating, enabling, materially
editing or seeding a rule primes it from each series' newest sample, so a condition that already holds
is not lost on a transition-only input. The boot seed ships `reachability-lost`, and the
NET_ADMIN end-to-end now closes the loop with no hand-raised alarm: unplug a simulated component, the
rule opens its alarm after 15 seconds (the test asserts none opens in the first 10) and the system,
whose one outage-impact role at quorum 1 the component fills, leaves healthy; plug it back and both recover. The
#557 fix rode along: every alarm read carries the component's uuid and name, and the console links by the
uuid again (the review caught stale fixtures hiding the break).
