## unbounded-window-overflows-timestamp (fixed; found by the #417 review)

- **Cue:** a duration setting (days, hours, seconds) with a minimum but no maximum, subtracted from
  now to make a cutoff that a delete or a filter compares against.
- **Failure:** a huge value (an admin's "keep forever", 999999999 days) puts the cutoff hundreds of
  thousands of years back. pgx's binary timestamptz encoding overflows int64 and Postgres receives
  a date in the far future, so `ts < $1` matches every row and the retention pass would delete all
  but each series' latest.
- **Fix:** a `maximum` on the setting's schema (retention stops at 36,500 days), and a range guard
  where the cutoff is computed that refuses an out-of-range stored value rather than acting on it
  (`internal/bus/retention.go`, `TestARetentionPassRefusesAWindowOutOfRange`).
