## fix-range-from-outpost-spans (fixed; found by the #1054 review)

- **Cue:** a quick fix or source rewrite whose replaced range is built from AST node
  locations alone (`Location()`, an outpost's min and max offsets), not from the source's tokens.
- **Failure:** an outpost's location never covers the brackets around it (Expr keeps no outpost for
  a parenthesized group), so the edit leaves one bracket behind: `(count(xs, p)) == len(xs)`
  "fixed" to `(all(xs, p)`, which no longer parses; one click turns a saving expression into a
  refused one.
- **Fix:** widen the range over the enclosing brackets by tokens before replacing it
  (`subject.bracketed`, internal/expression/diagnostics.go), and pin each bracketed shape as a
  fixture with its `fixed:` source, which the fixture runner recompiles.
