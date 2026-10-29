## locale-matched-error-text (fixed; found by the #1275 review)

- **Cue:** a Postgres error classified by its `Message` text (`strings.HasPrefix`,
  `strings.Contains` on `pgErr.Message` or `err.Error()`) to decide retry against refuse.
- **Failure:** a BYO server whose `lc_messages` is not English translates the message, the
  match fails, and the classification flips: #1275's first cut read a metric partition-routing
  miss as a refused CHECK on a German server, so every batch from a node with one far-dated
  sample was dropped whole, the #420 drop the slice existed to remove.
- **Fix:** classify on the structured fields: SQLSTATE plus `TableName`, `ConstraintName` or
  `ColumnName` (`isPartitionRoutingMiss`, internal/storage/metric_series.go), pinned by a test
  that builds the error with a translated message.
