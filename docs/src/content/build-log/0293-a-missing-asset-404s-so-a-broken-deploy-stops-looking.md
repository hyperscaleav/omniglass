---
title: "A missing asset 404s, so a broken deploy stops looking healthy"
date: 2026-08-26
refs: []
form: bullet
---

- **A missing asset 404s, so a broken deploy stops looking healthy**
  ([#778](https://github.com/hyperscaleav/omniglass/issues/778)). The SPA catch-all
  answered every miss under the console's own paths with `index.html` and 200, which made a
  partial deploy indistinguishable from a working one: a missing JS chunk arrived as HTML
  and the browser reported a syntax error at line 1, a missing face rendered wrong with no
  request having failed, and any monitor keyed on 4xx saw a healthy origin. The handler now
  reads the directories the build owns off the build output itself (`assets/`, and `fonts/`
  from `public/`) and 404s a miss inside one of them; a console route at any depth still
  renders the SPA. Derived rather than enumerated, so an asset kind the build starts
  emitting is covered with no code to remember it, which the `media/` case in the test rail
  pins. Keying off `Accept` was weighed and dropped: a deep link from curl sends `*/*` and
  must still render the console, but `*/*` is also what a script or stylesheet request
  carries, so the escape hatch the deep link needs re-admits the case the fix exists to catch.
