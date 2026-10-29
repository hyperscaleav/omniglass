## retry-blind-to-an-intermediate-state (fixed; found by the #1275 review)

- **Cue:** a retry that "makes the missing thing" by checking a catalog listing that hides an
  intermediate state (a partition still detaching, an index still building, a row soft-deleted),
  then treating "it exists" as success.
- **Failure:** the thing exists but serves nothing, so the retry is a no-op and the write misses
  again on every delivery until the batch exhausts them: #1275's roll saw a half-detached month
  as attached, and a batch with a sample in it was retried to death.
- **Fix:** detect the intermediate state and finish or undo it before re-making
  (`createMetricPartition` finishes a pending detach and drops the month first), pinned by a
  test that leaves the state behind on purpose.
