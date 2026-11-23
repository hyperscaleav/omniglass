---
title: "A page you may not read says so (#2442)"
date: 2026-11-23
refs: [2442, 1499]
---

As the seeded `viewer`, Settings showed in the sidebar, opened to "Loading settings." forever (the
API answered 403 and the query client retried it through its whole backoff), and `/secrets` or
`/users` by address silently redirected to Home.

A new `DeniedPage` primitive names the section, the `<resource>:<action>` permission it takes, and
links to Home and Profile, disclosing nothing behind the page. The route guard renders it instead of
redirecting; `PageGate` renders it for a page whose first read is a 403 (Settings uses it); and the
query client no longer retries a 403. Settings now carries the `settings:read` nav gate, and a nav
test fails any entry that declares no permission and is not marked `public`. The server's checks and
the wording of its 403 bodies (#1499) are unchanged. Tests: `RouteGuard.test.tsx`,
`PageGate.test.tsx`, `DeniedPage.test.tsx`, `nav.test.ts`, and the `e2e/denied.spec.ts` viewer walk.
