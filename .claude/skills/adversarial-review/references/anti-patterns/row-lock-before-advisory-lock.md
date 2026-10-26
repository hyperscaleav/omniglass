## row-lock-before-advisory-lock (fixed; found by the #504 review)

- **Cue:** a new `select ... for share` (or `for update`) on a row added to serialize a write
  against another, in a transaction that later takes an advisory lock another path takes BEFORE it
  locks the same rows (the label cascade takes `label/recompute` and then system rows for update).
- **Failure:** the two paths take the same pair of locks in opposite orders, so under load they wait
  on each other and Postgres aborts one with 40P01; nothing retries it, so an operator write fails
  as a 500. The new lock looks safe in isolation because shared row locks never conflict with each
  other.
- **Fix:** serialize with an advisory lock keyed on the row's id instead (`pg_advisory_xact_lock_shared`
  in the many-readers path, exclusive in the one writer), which the other path never takes, so no
  cycle forms (`lockSystemForStaffing`, internal/storage/roles.go, ADR-0506).
