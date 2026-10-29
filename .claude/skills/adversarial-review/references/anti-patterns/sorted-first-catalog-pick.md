## sorted-first-catalog-pick (fixed in its one known caller; found building #893)

- **Cue:** a shared test helper derives "a product or profile with X" by sorting names and taking
  the first match, and a caller then relies on a further fact Y the helper never checked (a plain
  http face, then an snmp face to attach to).
- **Failure:** a new catalog entry that sorts earlier and has X but not Y silently re-targets every
  caller; the one relying on Y fails, often in a tier the slice's author did not run (the netadmin
  impairment e2e met the Boreal Cast, which serves no snmp, in #893).
- **Fix:** each caller selects on everything it uses (the faces or drivers it touches), never on a
  narrower helper's first-by-sort result (`e2eSimulatedProduct(..., "snmp")`, `loginProfile(...,
  "POST /api/display/power")`).
