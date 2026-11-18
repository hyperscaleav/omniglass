---
title: "The principal becomes the user, in the console and docs (#2076)"
date: 2026-11-17
refs: [2076, 2036, 2075]
---

The IAM subject is a user, of kind person or machine (ADR-2036), and the console and docs now say so.
Admin's Groups tab becomes **User groups** at `/user-groups`, since IAM gave up the bare word group to
the fleet's entity groups; kind badges and the Users filter read person and machine (`kind:machine`)
through one `kindLabel`, while the wire still spells `human` and `service` until #2075 renames the
schema, API, CLI and permissions. The docs follow: principal becomes user, service account becomes
machine user, principal group becomes user group, and human as a kind becomes person; ADR titles and
anchors stay as written, and the glossary's group rows lose the IAM cross-reference. The vocabulary
denylist retires principal, principal group, service account and human-as-kind, allowing the
identifiers #2075 has yet to rename (`--principal-id`, `omniglass principal-group`,
`principal_group:*`), so a stray one in prose fails the lint.
