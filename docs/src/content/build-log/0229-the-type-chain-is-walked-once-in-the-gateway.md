---
title: "The type chain is walked once, in the gateway"
date: 2026-08-13
refs: []
form: bullet
---

- **The type chain is walked once, in the gateway**
  ([#695](https://github.com/hyperscaleav/omniglass/issues/695)). The naming half of this closed in
  [#702](https://github.com/hyperscaleav/omniglass/issues/702), which made the draft route answer with
  the name itself; what was left was `lib/typechain.ts`, the console's own first-non-null-wins climb of
  both type registries, running against `resolveTypeFacts` in Go for the ICON. A drifted stem produced
  a wrong address; a drifted icon produces a wrong glyph, so this is the same duplication with a much
  smaller consequence, and it was fixed the same way rather than differently.

  Both registry listings now carry `resolved_icon` beside the raw `icon`, and `lib/typechain.ts` is
  deleted along with the inline copy of the same climb that had grown in the Component Types page.
  The cost was weighed rather than assumed: the listing already loads the entire registry in one
  unfiltered query and the handler already indexes it to fill each row's parent name, so the walk is a
  pure pass over rows in hand and adds no query. Resolving PER ROW through the existing
  `ResolveTypeFacts` would have been one query per level per row, turning a one-query list into a
  hundred, so it was not done that way; the field is on the listing only, and a single-row write
  response does not carry it, because nothing reads an icon there.

  The two Go walks that remain are not an accident and are not left on trust. One reads a node at a
  time through a caller's own querier, because the name generator and the label renderer resolve facts
  INSIDE a transaction and must see the write still in flight; the other is a pure pass over a loaded
  listing. A test runs both over every seeded row of both registries and compares, which is what makes
  two walks in one package safe and is the instrument the old cross-language pair never had.

  It also settled a disagreement that was already live rather than hypothetical. The TypeScript walk
  treated an empty-string icon as ABSENT and kept climbing; the gateway's treats it as a value and
  stops, because the column is nullable and null is the "inherit" signal. `PATCH {"icon": ""}` stores
  that empty string today, so the two answered differently for a row an operator could actually
  create. The console now shows what the gateway resolves. Whether that patch should clear the field
  instead of storing a blank is a separate question about the write path, and it is left alone here
  rather than changed under cover of a read-side fix.
