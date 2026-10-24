---
title: "A restore clears the drafts it just discarded"
date: 2026-08-14
refs: []
form: bullet
---

- **A restore clears the drafts it just discarded**
  ([#741](https://github.com/hyperscaleav/omniglass/issues/741)). A registry blade seeded its edit
  drafts once, on entering edit. `:restore` discards an operator's fork, so the row changes
  underneath an open editor: the fields went on showing the values the operator had just thrown
  away, and the next Save would have written that fork straight back over the shipped ones. Fixed by
  hand on the location type blade in #710, where restore finally sat beside a field whose value
  restore changes; the same shape was live on the component type blade, unseen because nothing there
  had made it visible yet.

  It went into the SHARED blade primitive rather than being applied a second time by hand. Seeding a
  blade's drafts from its row is one concept with two triggers, and the edit slot (`lib/blades`) is
  the only shared thing that knows an editor is open, so a body now binds its seeder
  (`edit.bind({ seed })`) and the slot runs it on the way into edit, while a body that replaces the
  row asks for it again by name (`edit.reseed()`). Both registries with a restore leg are converted;
  binding is opt-in, so the thirteen blades that seed with a local effect on `editing` are byte-for-
  byte unaffected.

  **The system type registry needed no fix, contrary to the issue.** It has no restore leg at all:
  `system_type` has not adopted the fork (#655, ADR-0095), carries no `forked` field, and there is no
  `/system-types/{id}:restore` route, so the blade has no path that replaces its row. It inherits the
  primitive the day it adopts one.

  The test drives restore with a DIRTY draft on top of a fork and asserts the four fields, not that
  the request was sent: sending the request was never the broken half.
