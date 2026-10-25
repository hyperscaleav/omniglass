## fk-check-joins-lock-cycle (fixed; found by the #1142 review)

- **Cue:** a background insert written as "lock-free" (its own statement, holding nothing else) into a
  table with a foreign key to a row that a writer locks `FOR UPDATE` while it inserts into the same
  table, with a plain `exists (...)` read standing guard for the parent.
- **Failure:** the insert's foreign key check takes `FOR KEY SHARE` on the parent, so it waits on the
  writer, while the writer's own insert of the same key waits on the background tuple: a deadlock
  (40P01) that fails the background pass or the operator's write, or a 23503 when the writer deleted
  the parent.
- **Fix:** take the parent in the insert's own source `FOR SHARE SKIP LOCKED`, so a held parent is
  passed over and retried later (`markCalcWindowsDue`, internal/storage/calc_eval.go).
