## json-number-as-identity (fixed; found reviewing #1386)

- **Cue:** a key, an id or anything matched for equality is read out of JSON through a decode into
  `any` (or `float64`), then rendered back to a string: `json.Unmarshal` into `interface{}`, then
  `strconv.FormatFloat`.
- **Failure:** every integer past 2^53 rounds to its nearest float, so two distinct ids
  (`9007199254740993` and `9007199254740992`) read as one. Two rooms collapse onto one identity, and
  one room's values land on the other's binding, silently and only for the services whose ids are
  large.
- **Fix:** read an identity with a `json.Decoder` under `UseNumber` and keep the `json.Number`
  literal (`jsonLiteral`, internal/driver/interpret.go), and test with two adjacent ids past 2^53.
  A value read as a quantity (a metric) may stay float; a value matched for equality may not.
