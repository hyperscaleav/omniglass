---
title: "The detail surfaces caught up with the list"
date: 2026-08-04
refs: [553]
form: bullet
---

- **The detail surfaces caught up with the list** (#553). Unifying the list column exposed that the
  word "Name" then meant two things two clicks apart: a list column header rendering the label, and a
  blade fact rendering the kebab address. Seven blades called the address "Name" while the three
  estate blades already called it "Technical name"; they now all say "Technical name". Five registry
  blade titles rendered the address in the data face, so an operator clicked a row reading "Crestron"
  and got a panel titled "crestron"; the title now reads the same rule the row does. A source guard
  (`identity-vocabulary-guard.test.ts`) pins it, because the failure mode is a new page reaching for
  the wrong word and the page nobody wrote a test for is the page that drifts. Create forms still
  label the address "Name", which is the pre-existing convention on every page and is settled by the
  `display_name` to `name` rename, which moves both words at once.
