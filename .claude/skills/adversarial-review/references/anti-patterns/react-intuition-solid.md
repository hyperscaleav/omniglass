## react-intuition-solid (live, a false-finding class)

- **Cue:** a review claim about `web/src` that assumes React semantics: destructured
  props losing reactivity "bugs" that are actually correct Solid, missing dependency
  arrays, keyed-list re-render assumptions, `()` signal calls flagged as errors.
- **Failure:** wrong findings block correct code, or "fixes" break reactivity.
- **Fix:** load the `solidjs` skill before judging; a finding that assumes React
  semantics is invalid.
