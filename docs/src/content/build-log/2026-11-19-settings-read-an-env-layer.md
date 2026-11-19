---
title: "settings read an env layer"
date: 2026-11-19
refs: [2223]
---

A single setting now reads from the server's environment. `OMNIGLASS_SETTING__<NAMESPACE>__<KEY>`
feeds one key through a new `env` level between the settings file and the console binding: the
console still wins, so an operator's edit is never shadowed by a container's variable. Values are
coerced to the key's Go kind from the same reflected `Settings` struct the file layer validates
against, so a `[]string` takes a comma-separated list, a `bool` takes `true`/`false`, and a value
that does not coerce fails the boot naming the kind without quoting the value (it may be a bare
secret). The `profile` domain (`ui`, `keybindings`) is refused at boot, naming the variable: a
theme has no process-wide meaning. `cmd/configgen` renders one row per settable key in the
generated env-var reference so the table stays complete by construction, and the registry's drift
gate still fails when the facts move.
