---
title: "A placement change is its own gated act, `:move`, not a `PATCH` field"
date: 2026-08-08
refs: []
form: bullet
---

- **A placement change is its own gated act, `:move`, not a `PATCH` field**
  ([#627](https://github.com/hyperscaleav/omniglass/issues/627) Task 13, `b4449f3`,
  [ADR-0088](/architecture/decisions/#adr-0088-a-placement-change-is-an-authorization-act-so-a-move-is-its-own-verb)).
  `parent` and `location` leave the component, system, and location `PATCH` bodies entirely, a
  **wire-breaking** change the rolled-up PR title carries a `BREAKING CHANGE:` footer for. `:move`
  writes its own audit verb rather than the generic `update`, recomputes a platform-owned generated
  name at both ends of a component or system move, and closes a real gap the split surfaced: the old
  `PATCH`'s reparent branch only guarded a non-empty parent, so an explicit empty string cleared
  `parent_id` to root with no scope check at all, letting a scope-limited principal walk a row out of
  every subtree their grant ever covered; `MoveComponent` / `MoveSystem` now require the same
  all-scoped grant creating a root already needed. `MoveLocation` deliberately gains no matching
  clear-to-root capability, since none existed before this split either. A review pass found the
  console still `PATCH`ing `parent` through the now-`additionalProperties:false` body, `422`ing an
  entire location save over an unrelated field; fixed with tests that assert HTTP method and path and
  throw on any unexpected request, the shape that would have caught the original bug where three tests
  mocking `fetch` at the body level had not (`1382567`). Filed
  [#642](https://github.com/hyperscaleav/omniglass/issues/642): a location move leaves both the old and
  new ancestor chains' recorded health verdicts stale, a pre-existing gap this split carries forward
  rather than introduces or closes.
