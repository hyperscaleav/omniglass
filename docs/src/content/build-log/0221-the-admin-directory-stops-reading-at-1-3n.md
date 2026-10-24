---
title: "The admin directory stops reading at 1+3N"
date: 2026-08-13
refs: []
form: bullet
---

- **The admin directory stops reading at 1+3N**
  ([#671](https://github.com/hyperscaleav/omniglass/issues/671)). `ListPrincipals` drained its base
  query and then called `loadPrincipal` once per row, three statements each (the kind profile, the
  effective grants, the group memberships). Measured on the counting instrument that found it, the
  admin directory cost seven statements for two principals and sixty-seven for twenty-two, and it is
  the one directory that grows with the organisation rather than the estate, with no pagination to
  hide behind. It now costs **four, whatever the page size**: the base query, then one union over the
  three profile tables, one over the effective grants, one over the group memberships, assembled in
  memory by principal id. The shape is [#648](https://github.com/hyperscaleav/omniglass/issues/648)'s,
  reads keyed by id with the single-row function kept as the oracle, and `loadPrincipal` is unchanged
  and still the only path `GetPrincipal` takes.

  The projection did not narrow, and the console is why: the directory renders a grant COUNT and a
  badge per group a principal belongs to, so the list needs the same effective grants and memberships
  the detail blade does. A lighter row would have been the smaller fix and would have emptied two
  columns. Nothing about what the directory returns changed, which is what let the whole principal
  suite stay green with no expectation edited.

  The kind profile is a **union** rather than a query per kind present in the page. A branch per kind
  would have been flat in page size too, but its cost would have depended on the MIX, a directory of
  humans, service accounts and nodes costing two statements more than one holding only humans, and a
  count that varies with the fixture's composition is a count no assertion can state as a number. The
  cost fixture holds a node beside its humans to keep that honest rather than assumed, and grows the
  grants and the group memberships with the page as well as the principals, because a page whose rows
  carry no grants leaves the grant assembly flat by accident.

  Two gates, both of which the batch had to earn. `TestBatchPrincipalLoadMatchesSingle` holds the batch
  to the single-row loader field for field over a fixture carrying all three kinds, direct grants,
  inherited grants, both, neither, and a group with no display name; equality, not improvement, since a
  batch that answers differently from the loader every other read path uses is a second implementation
  rather than a fix. And the batch refuses what the oracle refuses: a principal of a profiled kind with
  no profile row is an error on both paths, not a directory row silently missing its username.
  `TestListPrincipalsCostIsPerRowToday`, which pinned the defect at its measured 1+3N and said in its
  own failure message that the fix was to delete it, is deleted; `TestListPrincipalsCostIsFlatInPageSize`
  takes its place beside the other nine.
