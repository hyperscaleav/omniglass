---
title: "An inherited component-type fact rides as omitted, never as an empty string"
date: 2026-08-12
refs: []
form: bullet
---

- **An inherited component-type fact rides as omitted, never as an empty string.** The component-type
  edit blade posted `stem`, `abbrev` and `icon` as raw signals seeded with `?? ""`, so a node that
  inherits a fact sent `""`. Those columns are nullable and the server's walk treats only NULL as
  inherit, while the patch coalesces, so the empty string wrote a real value that stopped the walk for
  that node and every descendant under it: silent, permanent, and on the facts the generated name and
  the abbrev-compacted render read. The console hid it in both directions, since the wire omits an
  empty icon and the client-side resolver treats `""` as falsy and keeps walking, so the page still
  drew the inherited glyph while the server no longer resolved one. `stem` also carries a minLength on
  the patch body, so a custom child that legitimately has none could not be edited at all: the save
  was a 422 before the handler ran. This is the original of the defect the system-type copy fixed
  alongside it, and it takes the same shape (empty means omitted, plus a guard test asserting no `""`
  ever rides the body). Clearing a fact back to inherit stays inexpressible from the console; the
  instrument for it is the three-state string sentinel already live on `label_rule` in the same
  handler, not the write mask, which stays scoped to nullable object fields.
