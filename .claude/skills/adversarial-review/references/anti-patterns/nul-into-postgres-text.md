## nul-into-postgres-text (fixed; found building #1145, regressed and fixed in the #311 review)

- **Cue:** text that came off a wire (a component's line, a telnet stream, a pushed body) is
  inserted into a Postgres `text` column, or into `jsonb`, with nothing replacing byte `0x00`.
  `strings.ToValidUTF8` does not help: NUL is valid UTF-8.
- **Failure:** Postgres refuses the insert ("invalid byte sequence for encoding UTF8: 0x00", or
  "unsupported Unicode escape sequence" in jsonb). On a multi-sink ingest the error fails the
  whole batch, which is redelivered and terminated, so every sample and event that rode with the
  line is lost; telnet's CR NUL makes the input ordinary rather than adversarial.
- **Fix:** replace NUL at the gateway's write, the one path every writer passes, over every text
  field and every string in the jsonb (`logline.Clean` and `logline.CleanAttributes`,
  internal/logline/logline.go, applied in `InsertLogLines`), and where text is read off the wire
  for a record whose insert path does not hold it.
- **Regressed by one-transaction landing (#311):** once an ingest batch lands all or nothing, a
  NUL in any lane's value (a pushed property, an event's message or attributes, a node's self-log
  of a device reply) fails every row that rode with it, after the push route answered 202. Fix:
  the gateway cleans every sample, event and node-log write the same way (`cleanPropertyWrites`
  and its siblings, internal/storage/nul_clean.go), and any value the database can refuse must be
  cleaned or refused before a composed transaction, never left to fail it.
- **Third instance (#1321 review, fixed in the same slice):** a notification template's string
  literal (`{{"\x00"}}`, `{{"\xff"}}`) rendered a NUL or invalid UTF-8 into a notification row
  inserted inside the alarm transition, so a raise or clear (and the ingest batch carrying it)
  failed. Fix: such output is a render failure recorded on the row (`renderNotification`), and the
  whole notification write runs under a savepoint that a database error rolls back to, so it can
  never fail the transition.
- **Third instance (#1321 review, fixed in the same slice):** a notification template's string
  literal (`{{"\x00"}}`, `{{"\xff"}}`) rendered a NUL or invalid UTF-8 into a notification row
  inserted inside the alarm transition, so a raise or clear (and the ingest batch carrying it)
  failed. Fix: such output is a render failure recorded on the row (`renderNotification`), and the
  whole notification write runs under a savepoint that a database error rolls back to, so it can
  never fail the transition.
- **Fourth instance (#1388 review, fixed in the same slice):** the fan-out recorded each unbound
  sample's raw wire name in the inbox's `keys_carried` text array before any catalog lookup, so a
  node-lane sample named with a NUL failed the inbox upsert and, with it, the batch's one
  transaction. Fix: only a name the lane's catalog holds is remembered (bounded at 32 per row), and
  a received identity is held to `driver.ValidIdentity` before it reaches a read or the inbox.
