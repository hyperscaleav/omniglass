## exists-probe-flattened-to-hash-join (fixed; found by the #417 review)

- **Cue:** a per-row correlated `EXISTS` (or `IN`) meant to be an index probe, run over many outer
  rows, guarded by a plan check that greps for "Seq Scan" or only for an index name.
- **Failure:** Postgres flattens an `EXISTS` sublink into a semi join, and at scale may run it as a
  hash over every row of the inner table. With `enable_seqscan` off the hash is fed by an index
  walked with no condition, so a "no Seq Scan" assertion still passes. The retention prune's
  newer-row check, written that way, hashed the whole metric table on every chunk on PG18 at 2.5M
  rows (1.5 s a chunk against 61 ms per-row).
- **Fix:** write the probe as a scalar subquery limited to one row (`(select 1 ... limit 1) is not
  null`), which stays a per-row SubPlan, and hold it with the accesspath instrument: the probe's
  scan uses the intended index with a condition bound to the outer row's columns
  (`internal/storage/prune_test.go`, `TestPruneSamplesReachesAnIndexOnEveryArc`).
- **Sibling (same review):** a per-row probe whose decisive test is a filter rather than part of
  the index condition (`n.id > c.id` on a `ts DESC` index) walks every row the index puts first. A
  probe down from the newest row passed every row a corrected clock had future-dated (2.5 s a
  chunk against 46 ms). Order the probe so its first index tuple is the likely answer
  (`order by n.ts limit 1`, a backward scan up from the candidate), and hold the direction in the
  plan test.
