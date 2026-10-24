---
title: "Driver tasks re-derive when a driver's spec changes (#823)"
date: 2026-10-23
refs: [823]
---

Deferred from the stateless arc's review, closed in the stateful rollup. Driver tasks were baked at
attach and never re-derived, so fixing a regex in a driver left the fleet emitting by the old one while
the Drivers page showed the fix, and the reply and error shapes #817 added to the official
`newtron-nvp` never reached endpoints attached before it. Now a driver update over the API, and the
boot seed's upsert (now one transaction per driver), re-derive every attached endpoint's driver tasks in
the same transaction: the tasks the spec derives are written where missing and the driver tasks it no
longer derives retire, while the reachability probe stays. Ids are content hashes, so an unchanged
function keeps its row, a re-seed that changes nothing touches no task and no endpoint, and an endpoint
whose tasks changed has its `updated_at` (the node's config generation) moved. The update's audit row
covers it. Proven over Postgres (`TestReseedWithNoChangeTouchesNothing`,
`TestSeededSpecChangeReachesAttachedEndpoints`, `TestDriverUpdateRederivesAttachedEndpoints`) and end to
end (`TestDriverFixReachesTheNextPoll`: a regex fix turns a faulting poll against a real line server into
a landed value on the next poll). The review that followed closed three races and a cost, each with a
Postgres test that failed first: an attach now reads the driver row `FOR SHARE`, so one racing an update
waits and derives the committed spec instead of keeping the old one for good
(`TestAttachDuringADriverUpdateDerivesTheNewSpec`, two interleaved transactions); the re-derive's endpoint
list takes `FOR KEY SHARE` in id order, so a concurrent delete is waited on and skipped rather than
failing the update on the task foreign key (`TestRederiveSkipsAnEndpointDeletedConcurrently`); and the
seed's upsert writes only when a field differs and re-derives only when it wrote, so an unchanged boot
leaves the driver and task rows' `xmin` as they were. Samples published under a retired task id in the
one-tick gap before the node's next pull are dropped as orphans, accepted and documented.
