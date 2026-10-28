## unindexed-fk-child-under-bulk-delete (fixed; found by the #417 review)

- **Cue:** a new bulk or retention delete on a table that another table references with
  `on delete set null` or `on delete cascade`, where no index leads with the referencing column.
- **Failure:** the foreign key's action runs once per deleted row, as an update or delete of the
  child by that column. With no index, each one scans the whole child table, even when every value
  is null. The log retention prune deleted `log_line` rows that `event.source_log_line_id`
  references: 27.7 s for a 2,000-row chunk over 200k events. The event table is never pruned, so
  the cost only grows, and a chunk can outlive its deadline and roll back forever.
- **Fix:** index the referencing column (partial on `is not null` when most rows have no link), in
  the same migration as the delete that needs it, and hold it with the accesspath instrument on
  the action's lookup (`internal/storage/prune_logs_test.go`,
  `TestPruningALogLineClearsItsEventsLinkThroughAnIndex`).
