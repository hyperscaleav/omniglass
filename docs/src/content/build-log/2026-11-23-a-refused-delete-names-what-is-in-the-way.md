---
title: "A refused delete names what is in the way (#2463)"
date: 2026-11-23
refs: [2463, 2423, 2485, 2488]
---

Cleaning up a demo needed the API: every delete asked through the browser's `confirm()`, and a
refusal was a sentence with no way forward ("still owns 2 endpoints", none listed). Three changes:

- **The 409 is typed.** A refused component, system or location delete now carries `blockers`
  (kind, id, name, label) and `blockers_hidden`, an additive field on the problem detail
  (`DeleteRefusal`). The gateway's `DeleteBlockersOf` reads each relation the delete refuses on
  (children, owned endpoints, staffed slots, what is placed in a location) and narrows each to the
  caller's read scope on the blocker's own kind in one statement (`inScopeIDs`); what the caller
  cannot read is counted, never named. Integration tests: `TestRefusedDeleteNamesItsBlockers` and
  `TestRefusedDeleteCountsWhatTheCallerCannotSee` in `internal/api`.
- **One confirm.** `confirmDelete` / `confirmAction` (`web/src/lib/confirm.ts`) open the one
  `ConfirmDialog`, drawn by a `ConfirmHost` under the router: title names the thing, body says what
  goes with it, the button carries the verb. All 38 native sites moved, and
  `native-confirm-guard.test.ts` bars `window.confirm` under `web/src`. A leave-with-drafts prompt
  went the same way.
- **The dialog clears the way.** Handed the delete as `run`, the dialog lists a refusal's blockers
  as links, each with its own two-step Delete, and retries in place. No cascade was added. A rule's
  Delete moved from More actions to the blade's destructive slot, where every other Delete sits.

Walkthroughs: `delete-what-you-made` (the component) and `delete-a-location-that-holds-a-system` (the location), split in #2813 so each fits the 60 second budget.
