---
title: "The command palette finds locations, systems, components and catalog entries (#2444)"
date: 2026-11-23
refs: [2444, 2485, 2429]
---

Ctrl-K used to search only the navigation. Typing a name now also lists the locations, systems and
components that hold it, and the products, drivers, rules and metrics, in groups, each fleet hit
with the place it sits in so the many same-named components are told apart. Enter opens the
highlighted row (a fleet entity at its page, a catalog entry at its drawer address through
`drawerHref`); Ask omnibot stays the last row and is never the default when anything matched.

One route, `GET /search`, answers it. The three fleet kinds are matched in the database inside the
caller's own read scope for each kind (the same scoped list walk every list uses, with the match and
the per-kind limit applied in SQL), so nothing outside the scope can appear and no collection is
fetched to the browser. The catalog kinds are small platform-wide lists, gated by their own read
permission, matched on the server. Typing is debounced and each keystroke cancels the request
before it.

The palette stops being a hand-rolled combobox: a new `Search` wrapper over Kobalte Search (list
rendered inside the dialog, first row the default, Enter takes it) is its field and list, which
removes one entry from the purge epic (#2488). Kobalte keys every collection section `""`, so only
one section heading survives a render; the wrapper draws each group's name on the group's first row
instead and reads it to a screen reader.

Recent items are kept in `sessionStorage` under the person's id, because the settings engine has no
per-user rung yet (the `user` tier is a fast-follow in the settings architecture); they last for the
tab's session.
