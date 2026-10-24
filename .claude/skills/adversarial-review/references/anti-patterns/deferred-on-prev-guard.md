## deferred-on-prev-guard (live; found building #759)

- **Cue:** a Solid `on(deps, fn, { defer: true })` callback gating its action on the
  truthiness of its `prev` argument (`if (prev && ...)`).
- **Failure:** defer skips the effect's first execution, so the first REAL invocation
  receives `prev === undefined` even though a genuine transition triggered it; the guard
  silently swallows exactly the first transition. Found live on #759's param-strip effect,
  where `begin()` landed before the effect's first capture and the Cancel transition
  arrived with `prev` undefined, so the strip never fired.
- **Fix:** a deferred `on()` callback only ever runs on a change; branch on the current
  value alone (`if (!editing && ...)`), never on `prev` truthiness. If the previous value
  genuinely matters, capture it in a signal the effect owns.
