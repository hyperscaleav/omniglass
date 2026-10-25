## memory-mirror-of-a-bounded-table (fixed; found by the #1024 review)

- **Cue:** an in-process set mirroring rows of a table the store caps or ages out, added to on
  every write and trimmed only on a rare event, on a path a stranger can reach.
- **Failure:** the table stays bounded while the mirror grows with every distinct key a stranger
  or a scanner supplies, until the process runs out of memory, taking the API and the embedded
  bus with it.
- **Fix:** make the mirror the table's own rows, reloaded after every write
  (`loadUnclaimed`, internal/api/webhooks.go), or cap it with the table's own eviction.
