---
title: "Console: the list surface splits into a shell plus swappable bodies"
date: 2026-07-07
refs: []
form: bullet
---

- **Console: the list surface splits into a shell plus swappable bodies.** `ListShell` owns the chrome every
  list wears (the FilterBar chip state and client-side predicate, the card, the error banner, a trailing
  action slot) and hands its body the filtered rows; `FlatList` is the flat body (a sortable table, an
  optional row-to-Drawer detail, an optional create Drawer, an optional footer). The Audit page becomes a thin
  `FlatList` config, behaviour-preserving. This is the primitive-first factoring of the four list idioms that
  had grown up (tree ListView, the Users split-panel, the Roles catalog, the Audit table): the tree pages move
  onto `ListShell` + a `TreeList` body, and Users onto `FlatList`, in follow-up slices; Roles stays the
  read-only catalog until custom-role CRUD. Proven by the Audit component test (unchanged behaviour on the new
  primitives) and the existing web suite.
