## nul-into-postgres-text (fixed; found building #1145)

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
