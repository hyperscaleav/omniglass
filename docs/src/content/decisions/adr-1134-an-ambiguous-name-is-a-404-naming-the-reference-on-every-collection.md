---
title: "An ambiguous name is a 404 naming the reference, on every collection"
date: 2026-11-13
status: "Accepted"
summary: "A name that more than one visible row answers to now gets the same refusal on every collection (#1134): a 404 naming what was looked up and listing each candidate in a spelling that resolves on the same route. A component, system or location, which answered 409, moves to that shape: each candidate is its dotted address with its uuid beside it. A candidate the caller cannot read is neither listed nor counted, and scope still decides before ambiguity does"
refs: [1134, 1133]
---

- **Date:** 2026-11-13 | **Status:** Accepted | **Pages:** [api](/architecture/api/),
  [core entities](/architecture/core-entities/), [CLI](/guides/cli/)
- **Decision:** (#1134, the follow-up ADR-1133 named.)
  1. **One status for a repeated name.** The tree entities (component, system, location) answered an
     ambiguous bare name with a 409, while an endpoint, secret, variable, file, group or principal
     answered a 404 (ADR-1133). The tree entities move to the 404: the reference as typed resolves to
     no one row, which is what a 404 says, and a 409 reads as a conflict with a stored row, which
     nothing here is. `mapRefErr` maps `ErrAmbiguousName` through the one message builder the #1133
     refusal already used, so the two cannot drift apart.
  2. **A candidate is spelled so it resolves.** For a tree entity that is its dotted address with the
     uuid beside it (`campus.room-a.$comp.display-1 (<uuid>)`): the address says where the row sits,
     the uuid survives a rename, and either half is accepted on the route that refused the name. The
     addresses are rendered by the same `PathsOf` walk a read uses, inside the request's transaction.
  3. **Redaction is unchanged.** Candidates are narrowed to the rows the caller may read before
     ambiguity is judged, so a candidate the caller cannot read is neither listed nor counted. A path
     with no caller scope to filter by (the three `*NameTaken` advisories) still names no candidate and
     says only that more than one row answers to the name.
  4. **No compatibility path.** The 409 is removed, not kept beside the 404, per the pre-deployment
     stance. A create that collides on a name it is about to write (a `:move`, a rename) stays a 409,
     since that is a true conflict with a stored row.
- **Context:** ADR-1133 gave every other collection a 404 that lists its matches and left the tree
  entities on their older 409, a split an operator and a generated client could only learn by trying.
  The 409's candidates were bare uuids, which resolve but say nothing about which row is which.
