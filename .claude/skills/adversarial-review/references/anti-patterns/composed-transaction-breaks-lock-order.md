## composed-transaction-breaks-lock-order (fixed; found by the #311 review)

- **Cue:** several write paths that each follow the platform's lock order (rule rows, then
  marks, then series and health locks) composed into one transaction by running them one
  after the other.
- **Failure:** the composition holds the first path's late locks while the second takes its
  early ones. `LandBatch` judged its property lane (raising an alarm took the owner's health
  lock) and then took the metric lane's rule rows FOR SHARE, so it held health while waiting
  on a rule row a rule edit held FOR UPDATE, and the edit, wanting that health lock next,
  deadlocked with it; a rule write has no deadlock retry, so the operator's edit failed.
- **Fix:** take every path's early locks for the whole transaction first, in the documented
  order, and let each path re-take them (already held) as it writes (`lockInOrder`,
  internal/storage/land_batch.go). Pin it with a test that holds the contended row and
  probes the late lock while the composition waits
  (`TestLandBatchTakesItsRuleRowsBeforeAnyHealthLock`).
