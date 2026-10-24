---
title: "Every identity exception is named explicitly"
date: 2026-08-04
refs: [552]
form: bullet
---

- **Every identity exception is named explicitly** (#552). The key classification guard only inspected
  tables carrying a `name` column, so it saw 23 of 51 and was blind to every table identified by
  something else: `human` by a username, `blob` by a sha256, `task` by a content hash. Absence of a
  `name` is not evidence of absence of an identifier. It now declares one of four identity shapes for
  every table (key-bearing, keyspace, a human identifier that is not a key, id-only) and fails on a
  table with none or with more than one, so a new table is a failing test until somebody classifies
  it. Proved non-vacuous in both directions: removing a table fails, and declaring one twice fails.
