## per-record-fault-flood (fixed; found reviewing #1386)

- **Cue:** a loop over the elements of an input (records in a payload, lines in a batch, rows in a
  post) appends one fault, warning or event per element that fails, and the caller turns each into
  a published occurrence or joins them into one attribute.
- **Failure:** one upstream change (a renamed id field) fails every element at once: ten thousand
  records are ten thousand `collection-failed` events and node warnings per poll, or a webhook raw
  line whose fault attribute grows without bound. The flood drowns the one fact an operator needs.
- **Fix:** collapse per-element faults to one per kind per run, counting them and naming the first
  few ("missed on 10 of 30 records (records 1, 4, 7, ...)"), and cap the elements read
  (`faultTally` and `MaxRecords`, internal/driver/interpret.go). Test with many failing elements
  and assert one bounded fault.
