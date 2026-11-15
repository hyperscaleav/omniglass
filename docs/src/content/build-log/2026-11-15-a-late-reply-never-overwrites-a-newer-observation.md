---
title: "A late reply never overwrites a newer observation (#1606)"
date: 2026-11-15
refs: [1606, 1176]
---

A response-settled command's reply was written as an observation stamped at the database's now, so a
retried exchange whose reply reached the server after the device had pushed a newer value put the older
value back on top until the next poll. Settlement now records the reply as an observation only when the
target was not observed after the command's latest delivery; the command still settles or fails on the
reply either way. `TestALateReplyDoesNotOverwriteANewerObservation` failed before the change (the latest
read was the reply's `hdmi3`, not the push's `dp1`) and passes after;
`TestAReplyIsRecordedOverAnObservationOlderThanTheExchange` holds the other side, that an observation
from before the exchange does not stop the reply being recorded. The decision is ADR-1606.
