## nonfinite-float-into-json (fixed; found building #1145)

- **Cue:** a value parsed with `strconv.ParseFloat` (which accepts `NaN`, `Inf` and `Infinity`
  without an error), or scaled past the float range, is put into a map that is then
  `json.Marshal`ed, behind a helper that panics on the marshal error.
- **Failure:** a component reporting `NaN` (or a scale overflowing to `+Inf`) makes
  `json.Marshal` fail with "unsupported value", the helper panics, and nothing on the node's
  interpreter path recovers: the node process exits, and exits again on every restart while the
  component keeps reporting it. Every endpoint on that node stops collecting.
- **Fix:** refuse a non-finite value where it is parsed (`math.IsNaN || math.IsInf` gives "not a
  finite number"), as a calc rule's output is refused, and let a marshal error fault the one emit
  instead of panicking (`Field.resolve`, internal/driver/interpret.go).
