---
title: "The create form asks what and where first"
date: 2026-08-11
refs: [688]
form: bullet
---

- **The create form asks what and where first** ([#688](https://github.com/hyperscaleav/omniglass/issues/688),
  the eighth and last slice of [#657](https://github.com/hyperscaleav/omniglass/issues/657),
  [ADR-0104](/architecture/decisions/#adr-0104-a-create-form-shows-the-name-it-can-know-and-never-mints-one-to-preview-it)).
  Eight slices built a generator the console could not reach. Two of the three estate create forms
  derived the name from the display name and refused to submit without one, so a row created through
  the console arrived operator-named whether the operator meant that or not, and the third could leave
  the field blank but said nothing about what would land there. All three now lead with the
  **classification** and the **placement**, because those are what the naming and labelling rules read,
  and the identity section that follows shows what the platform will call the row.

  What it shows is the SHAPE, and the ruling this slice owed was where to draw that line. The stem
  falls out of the classification the operator has just chosen; the ordinal is the lowest free number
  among the live siblings in the placement bucket, allocated under an advisory lock inside the
  create's own transaction, so it does not exist until the row does. The form writes it as the token
  `n` (`display-n`, `boardroom` for a mint that suppresses its first ordinal, `n` alone for a
  positional type), always with the sentence that makes it honest. A draft-preview verb that minted
  and rolled back was refused twice over: its answer is provisional, since another create can take the
  ordinal in between, and the rolled-back mint takes the lock real creates need, so previewing on
  every picker change would serialise the estate's creates behind a UI affordance. Re-rendering the
  label rule in TypeScript was refused outright as a second implementation of the engine slice 3 swept
  42 copies of, so the label is not previewed at all.

  The placement is shown as a **path** beside the field and never as a prefix inside it. The
  sub-issue asked for the ancestry as a read-only prefix on the name, which two slices had already made
  meaningless: names became scoped to placement, so a name no longer contains its ancestry, and
  printing it back into the field would restore exactly the redundancy the scoping removed.

  The three forms converge on one `CreateIdentity` section rather than a third copy, the two type
  registries' inheritance walks converge on one primitive with the stem as their third consumer, and a
  name is now optional on all three, required only where nothing will generate one (an unclassified
  system, a `location_type` with no name rule, a `component_type` chain with no stem), which the form
  names rather than just refusing. The browser tier grew the case that closes the epic: it reads the
  shape the console resolved in the browser, creates with the name left blank, and asserts the row
  landed with that stem and an ordinal the console could not have known. That runner also moved onto
  the dockerised, no-published-ports recipe the docs capture already used, since the old one could not
  start whenever another worktree's dev stack held 5432.
