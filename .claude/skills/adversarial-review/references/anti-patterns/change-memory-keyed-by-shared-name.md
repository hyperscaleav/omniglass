## change-memory-keyed-by-shared-name (fixed; found building #1145)

- **Cue:** a per-run change memory ("fire only when this differs from last time") keyed by a name
  that several of the remembered things can share: a log line emit's empty name, two emits of one
  event type in one function.
- **Failure:** the two things overwrite one slot. On an unchanged run the first differs from what
  the slot holds (the second's content), lands again, and the commit stores it; the next run the
  second does. One duplicate per run, forever, into a table nothing prunes.
- **Fix:** key the memory by the emitter's own identity (the emit's index in its function, carried
  from the interpreter on `driver.Emitted.Emit` and `collection.Sample.Emit`), and test with two
  emits that share a name (`occurrenceSlot`, internal/node/schedule.go).
