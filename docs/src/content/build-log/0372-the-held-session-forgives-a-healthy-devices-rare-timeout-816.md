---
title: "The held session forgives a healthy device's rare timeout (#816)"
date: 2026-10-23
refs: [816]
---

The re-review of the review fixes, each with a regression test that failed first. The previous
commit's title overclaimed: a line the device sends after the drain and before it reads the request
is still taken as the answer, a window #817's reply matching closes (ADR-0150 point 5 now says so).
An answered exchange now forgives the failures before it, so one missed answer in fifty never reads
down (`TestSessionSporadicTimeoutsNeverReadDown`, 250 good exchanges); a down session publishes
nothing until a returning connection proves itself by an answer, or by outliving the stable window
unless answers were what failed, so an accept-then-drop or hung device no longer flaps up and down
(`TestSessionHungDeviceStaysDownWithoutFlapping`, `TestSessionDownReadsUpOnceTheConnectionProvesStable`).
On the wire: a late interrupt mid-answer keeps the partial bytes (`TestLineConnLateInterruptKeepsThePartialReply`),
only a passed read deadline is an interrupt so a failed keepalive keeps its cause, a caller's own
deadline is not recorded as the device's timeout, and the wait for a first dial has its own budget
(`TestSessionFirstDialWaitHasItsOwnBudget`).
