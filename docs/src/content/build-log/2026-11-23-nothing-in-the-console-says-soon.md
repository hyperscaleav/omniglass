---
title: "Nothing in the console says soon (#2445)"
date: 2026-11-23
refs: [2445, 2449, 2450, 2446]
---

The console no longer shows an unfinished marker. The sidebar's Dashboards, Learn and Declared
entries (stub pages behind a `soon` tag) and the orphan `/log-types` stub are gone, with
`SectionStub`, the `Soon` component, the catalog table's `soon` slot flag and `CATALOG_STUB_PATHS`.
Dashboards and Learn return as live entries when #2449 and #2450 ship; Declared waits for the
declared projection. The outpost drawer's "driver fn soon" chip is gone: the Tasks column names what
the task runs today.

An address with no page now lands on its own Not found face (not a placeholder with a badge): the
address that was asked for, links to Home and Explore, and the command-palette hint.

A guard (`web/src/no-soon.test.ts`) fails any rendered "soon" word, or "not built yet" / "not wired
yet", under `web/src`, comments aside. One line is allowlisted: Platform health's `NOT_WIRED` string,
owned by #2446. The route smoke (`web/e2e/routes.spec.ts`) now asserts no route, the not-found
address included, and no outpost drawer renders the word.
