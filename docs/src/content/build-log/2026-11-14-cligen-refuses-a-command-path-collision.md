---
title: "make gen refuses two operations that derive one CLI command (#1358)"
date: 2026-11-14
refs: [1358, 1341]
---

`cligen` assigned each operation to its command path without checking whether another already held
it, so two operations deriving one path left a single command in the CLI and its reference and the
other silently gone. It surfaced as `POST /auth/me/action-rules/{name}:test` and
`POST /auth/me/action-rules:test` both becoming `auth me action-rule test`. `tree()` now returns an
error naming both operations (operationId, method, path) and the shared command path, and `make gen`
fails on it. A path that is both a command and a group (`component property` beside
`component property list`) stays allowed. The one collision in the current API, `webhook get`
(`verify-webhook` and `receive-webhook`, a POST on an item route deriving `get`), is now
`webhook verify` and `webhook receive`. Unit tests cover the duplicate, distinct paths, a command
beside its group, and the committed spec generating cleanly.
