---
title: "An ambiguous placement names the rows it matched"
date: 2026-08-12
refs: []
form: bullet
---

- **An ambiguous placement names the rows it matched.** Every building's first floor may be named `1`,
  so a create or a move that binds a location by bare name matches one row per building and answered
  `"1" is ambiguous for location (matches )`: the operator was told their input was ambiguous and
  handed nothing to disambiguate with. The list was empty on purpose, and the reason had expired. It
  was stripped because a candidate could be a row the caller holds no grant to read, and disclosing
  that such a row exists, even only as a uuid in a 409, is the leak the non-disclosing not-found
  exists to prevent; the slice before this one narrowed the bind to the caller's own read scope on the
  referenced tier, and the reference primitive filters the match set through the scope tree BEFORE it
  judges ambiguity, so every candidate is now a row that caller already may read.

  Only that one seam changed. The redaction does two jobs, and each of the other call sites still
  needs it, so it kept them: the three availability advisories resolve scope-blind by design (they
  answer about the placement bucket asked about, not the caller's grant), and the component end of a
  membership write, a role write and a tag resolve holds only a scope resolved for the other tier,
  which can never narrow a component lookup. The redaction's second job, folding a structural
  path miss down to the bare sentinel so a create's missing location stays the 422 it is rather than
  becoming a blanket 404, is now its own function and is what the placement bind still calls.

  The listed candidates are uuids, and a uuid resolves this same reference, so the answer is
  actionable rather than only descriptive. So is the dotted address the issue assumed did not exist
  here: the body reference has parsed one since addresses landed, which the slice pins with a create
  that binds `hq.wing-a.west.1` where the bare `1` is refused.
