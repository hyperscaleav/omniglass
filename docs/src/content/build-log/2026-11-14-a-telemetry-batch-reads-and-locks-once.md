---
title: "A telemetry batch reads and locks once, and commits once (#1928)"
date: 2026-11-14
refs: [1928, 311, 1279]
---

A node batch cost 19 statements, and six of them repeated work the batch had already done: the
lanes read the event rules and probed the calc marks again after the batch's lock pass had taken
them, the property lane took a series lock the batch held, the consumer read the catalogs'
fingerprint on every batch, and the endpoint's seen stamp was a second transaction of its own. The
batch's transaction now carries the rules, marks and series locks it took, so no lane repeats them;
the seen stamp is the transaction's last write; the fingerprint is read at most every five seconds;
and a calc mark already pending writes no new tuple while still holding its row lock. A node batch
now costs 13 statements and a collection report batch 10. Storage tests count `LandBatch`'s
statements (15 steady, 80 with rules judged), pin the stamp inside the transaction and the repeated
mark's unmoved tuple; a bus test pins the fingerprint's interval. The decision is ADR-1928.
