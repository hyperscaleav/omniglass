## deleted-owner-sinks-the-batch (fixed; found by the #1369 review)

- **Cue:** an in-memory accumulator (a window of counts, a tally) flushed for many owners in one
  gateway transaction, each row keyed by an owner id captured when it was counted, with nothing
  dropping an owner deleted since; worse when the accumulator carries an owner forward (a zero it
  owes, a count it retries).
- **Failure:** the gateway resolves each row's owner and refuses the first that no longer exists, so
  the transaction rolls back whole. Deleting one webhook-fed component within a report window of its
  last post lost every other component's `webhook-post-count` and `webhook-refusal-count` for that
  window, and the trailing zero the window owed the deleted endpoint sank the next window too.
- **Fix:** land one transaction per owner, dropping an owner that is gone, so a refusal costs only
  its own rows, as the node lane drops an orphaned batch alone (`recordOrphan`,
  `internal/bus/consumer.go`); or resolve the owners before the write and forget the missing ones.
  Pin it with a write carrying a deleted owner beside a live one
  (`TestADeletedComponentCostsOnlyItsOwnWebhookCounts`, `internal/storage/collection_report_test.go`).
