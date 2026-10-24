---
title: "Identity: the principal lifecycle in the console, UI"
date: 2026-07-09
refs: []
form: bullet
---

- **Identity: the principal lifecycle in the console, UI.** The user blade's footer action bar presents the
  lifecycle by state: the left slot is the reversible toggle (Disable / Enable, or Restore for an archived
  account), and the kebab holds the escalating red steps (Archive when live, Purge when archived and the
  caller holds `principal:purge:admin`), each confirmed. The detail fetches the principal by id (not the
  directory list, which hides archived) so a just-soft-deleted user stays resolvable, and a **Show
  archived** toggle on the Users directory (a new `include_archived` list param) surfaces hidden accounts
  to re-find one. `BladeSecondary` gains a red tone and the destructive slot a restore tone; the web `can()`
  already honours the three-token `principal:purge:admin`. This slice also **renamed the soft-delete verb
  deactivate to archive** (restore, not reactivate) so the ladder reads pause (disable) to remove (archive)
  to destroy (purge), instead of two pause-synonyms; the column, endpoints, capability, and list param follow.
  Proven by the web suite (the live and archived footer states and the show-archived flow) and a storage test
  for the include-archived list; completes
  [#143](https://github.com/hyperscaleav/omniglass/issues/143) / [#146](https://github.com/hyperscaleav/omniglass/issues/146).
