---
title: Build log
description: "The slice-by-slice build history: one entry per merged vertical slice, oldest first."
# The entries are not in this file. Each is one file under docs/src/content/build-log/,
# spliced in where ::build-log sits by docs/src/plugins/remark-fragments.mjs. Add an
# entry by adding a file; internal/docslint fails an entry written here.
---

Each entry below is one merged vertical slice, oldest first, logged as it landed. The live
map of what is built sits on [implementation status](/architecture/status/); this page is
the history behind it. Entries are append-only and quote the vocabulary of their day, so
retired terms appear here exactly as they shipped.

The code is built one **vertical slice per PR**, each a thin cut through the whole stack rather than a
horizontal layer. Slices are logged here as they land; a page's badge only flips once *all* of its
capabilities ship, so an early slice can prove a seam without moving any page off `Design`.

::build-log
