## window-misses-a-later-commit (fixed; found by the #1626 review)

- **Cue:** a periodic count over rows stamped with `now()` (a transaction's start), windowed as
  `stamp > previous read and stamp <= now()`, while the stamping transaction can still be open
  when the read runs.
- **Failure:** a command redelivery stamps `dispatched_at` at its transaction's start and commits
  after a health read. That read cannot see the uncommitted row, and the next read's lower bound
  is already past the stamp, so the redelivery falls between two windows and is never counted.
- **Fix:** read the window a fixed lag behind now (both bounds shifted, so windows still tile and
  each row counts once, up to the lag late): `redeliveryCountLag`,
  internal/storage/command_health.go, pinned by
  `TestCommandRedeliveryCommittedAfterAReadIsCounted`.
