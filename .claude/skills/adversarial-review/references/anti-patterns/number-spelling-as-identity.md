## number-spelling-as-identity (fixed; found reviewing #1653)

- **Cue:** a number from operator JSON is kept as its `json.Number` literal and then compared, as a
  string, with an identity stored in canonical digits, while the schema that accepted it is an
  `integer` (huma accepts any value equal to its truncation, so `2.0` and `20e-1` pass).
- **Failure:** `outlet-off` with `{"outlet": 2.0}` passes the schema, the driver renders `2.0` and the
  PDU switches outlet 2, but the hold set compares `"2.0"` with the dependency's `"2"`, finds
  nothing, and the codec on that outlet is cut with no hold, so it pages as down.
- **Fix:** read a whole number as its digits before matching (`wholeDigits`,
  internal/storage/holdset.go), keeping the literal for a fraction or anything past 2^53 so
  [json-number-as-identity](json-number-as-identity.md) still holds, and test every spelling the
  schema accepts (`TestOutletOffHoldsHoweverTheOutletIsSpelled`).
