## shared-spec-written-back-by-create (fixed in this slice)

- **Cue:** a package-level slice or struct value (a calc rule spec, its `Inputs`) passed to a
  gateway create that resolves names to ids and writes them back into the argument.
- **Failure:** the first database's key ids ride into the second: the second create resolves
  by the stale id and refuses ("not a registered property or metric"). Passes alone, fails
  in a package that seeds twice (#2757, internal/devseed room rules).
- **Fix:** build the spec fresh per call (a function, as `roomRules()` in internal/devseed/plan.go).
