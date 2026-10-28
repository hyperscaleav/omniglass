## fact-sized-template-work (fixed)

- **Cue:** a new key added to a `sandbox.Data` (a fact map) whose value has no length bound
  (operator or device free text: an alarm message, a description, a sample value), or a
  variadic builtin admitted to the sandbox grammar, with no cap on fact size or template
  length.
- **Failure:** the grammar bounds constructs, not work per call. text/template evaluates every
  argument of a variadic builtin (`eq`) before calling it, so
  `{{if eq .Message (upper .Message) (upper .Message) ...}}` holds one copy of the fact per
  argument. A 6.8 KB template over a 1 MB message peaked near 400 MB; a 1 MB body would
  OOM the binary. The output cap never sees it because nothing is written. Work is template
  length times fact length, and the grammar alone bounds neither.
- **Fix:** bound both at the primitive, for every caller: `sandbox.Engine.Parse` caps the
  source (`MaxSource`) and a call's arguments (`MaxArgs`), `sandbox.Execute` cuts each fact
  to `MaxFact` on a rune boundary (`BoundFacts`), pinned by
  `TestWorkIsBoundedWhateverTheFactsHold` in `internal/sandbox`. Found by the #1319 review,
  fixed in the same slice.
