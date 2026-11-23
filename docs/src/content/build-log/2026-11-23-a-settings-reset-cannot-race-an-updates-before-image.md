---
title: "A settings reset can no longer race an update's before-image (#2221)"
date: 2026-11-23
refs: [2221, 1444, 1952]
---

A setting update read its audit before-image with a plain `SELECT` under the (scope, namespace)
advisory lock, but a factory reset (`DeleteAllSettingBindings`) deletes by scope and takes no
advisory lock. A reset committing between the read and the upsert left an audit row claiming the
value moved from the deleted state. The before-image read is now `for update`, so the reset waits
on the row. Regression test: `TestSettingsUpdateImageSurvivesAConcurrentReset`.

The audit page now names both reasons an advisory lock stands in for a row lock: an append-only
series, and a create-or-update path whose row may not exist yet.
