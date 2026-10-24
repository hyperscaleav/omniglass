---
title: "Console: the read -> Edit -> Save contract on a blade footer action bar"
date: 2026-07-08
refs: []
form: bullet
---

- **Console: the read -> Edit -> Save contract on a blade footer action bar.** Every detail blade opens
  read-only; the header is chrome only (back, full-page, close), and a `createEditSlot` per blade drives a sticky
  **footer action bar** that `BladeStack` renders: a destructive action on the left (always available), secondary
  actions in a `⋯` kebab, and Edit on the right (which swaps to Save / Cancel in edit mode). The body reads
  `useBladeEdit().editing()` to switch its sections read-only vs live and registers the whole footer through
  `bind` (`editable`, `save`, `cancel`, `destructive`, `secondary`), so editability and the destructive label
  follow the caller's permission and a read-only blade (a role) registers nothing and shows no bar. In edit mode
  the profile becomes inputs, member add / remove stages locally, and the `GrantBuilder` gains a `bind` so its
  diff commits as part of the one blade Save; the destructive action (Delete for a group, Disable / Enable for a
  user, per the accounts-never-deleted invariant) confirms and is reachable straight from read mode. Save commits
  the staged session together and Cancel reverts. A single consolidated audit event and a config restore point
  (one backend action per Save) remain deferred to a batched endpoint; the user archive / purge lifecycle is
  its own slice. Proven by the web suite (the edit-slot, the `BladeStack` footer-bar chrome, and the Group / User
  read-vs-edit tests) and tsc.
