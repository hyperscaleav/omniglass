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
- **Second instance (#1743 review, fixed in the same slice):** the sim push face posted one batch
  per system its grant names and returned on the first refusal. A grant outlives the system it
  names (`principal_grant.scope_id` is text, not a foreign key), so deleting one system in a lab
  404'd its push every tick and starved every system after it in the grant's order. Fixed by
  pushing each system on its own and collapsing the tick's failures into one error, the first
  named and the rest counted (`pushOwners`, internal/sim/push.go;
  `TestPushFaceKeepsPushingPastAGoneSystem`).
