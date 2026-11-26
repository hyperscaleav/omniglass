---
title: "A held session's first connection is not a collection failure (#2755)"
date: 2026-11-26
refs: [2755, 2754]
---

A poll on a held session (a websocket, an NVP line) that was still making its first connection used to wait out
its timeout and then land a `collection-failed` naming the session as `establishing`, so a healthy device's Activity
opened with a row of red flags before its first sample. A session that has never been up now answers such an exchange
with a typed error, and the outpost holds the poll instead: no `collection-failed`, no backoff, counted in
`poll-held-count`, and run again at its next slot. A run now in that window answers that the session is still making
its first connection. A poll on a session that reads `recovering`, `down` or `rejected` still lands its failure.
