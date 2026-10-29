## verb-swallows-a-sweeps-owed-effect (fixed; found by the #1382 review)

- **Cue:** a deadline row that a background sweep ends with an effect (an announcement, a
  release, an audit row), and an operator verb on the same row that deletes or overwrites it
  without asking whether its deadline has already passed.
- **Failure:** an alarm snooze passed its deadline with its condition still raised, and an
  operator unsnoozed (or re-snoozed) it in the seconds before the sweep reached it: the verb
  deleted the row as "no snooze in force", wrote nothing, and the sweep then found no row, so
  the people the deadline owed a new announcement were never told.
- **Fix:** under the row's lock, the verb first ends an expired row exactly as the sweep would,
  effect included, and only then does its own work (`endExpiredTx` in
  internal/storage/alarm_snooze.go, called by `SnoozeAlarm`, `UnsnoozeAlarm` and the sweep).
