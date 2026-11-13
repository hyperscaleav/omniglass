## field-of-a-secret-list-echoed-on-parse (fixed; found by the #1743 review)

- **Cue:** a parser of a delimited value that carries secrets (an env list of `name=profile:token`,
  `user:password@host`, `key=secret` pairs) that names the malformed entry in its error by a field
  it cut out, where the cut falls back to the whole entry when its delimiter is missing.
- **Failure:** `strings.Cut` returns the whole input as its first result when the separator is
  absent, so an entry missing its `=` makes the "name" the entire entry, token included, and the
  refusal prints it into the process log at startup: `OMNIGLASS_SIM_PUSHERS` given
  `booking-service:ogp_...` logged the pusher's bearer token.
- **Fix:** name a malformed entry by its position, and by a field only once the delimiter that
  bounds that field was found (`ParsePushers`, internal/simnode/pushers.go), with a test that feeds
  an entry missing each delimiter and asserts the secret is absent from the error
  (`TestParsePushersKeepsTheTokenOutOfItsError`).
