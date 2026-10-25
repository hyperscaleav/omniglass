---
title: "No help text ever shows a credential (#979)"
date: 2026-10-25
refs: [979]
---

Every command's `--help` printed the live bearer token. The root's `--token` flag took
`OMNIGLASS_TOKEN` as its default when the tree was built, and cobra prints a flag's default in
every help page, so `(default "ogp_...")` sat under every command, ready to be pasted into a
ticket; the synthetic user (#961) found it on five missions running. `--server` had the same
shape, so a server URL carrying a password in `OMNIGLASS_SERVER` leaked the same way, and the
generated CLI reference was a function of whatever the person running `make gen` had exported.

The connection flags now resolve at run time (`flagOrEnv` in `internal/cli/api_hooks.go`): the
flag when given, an explicit empty value included, else the variable, else the literal default.
Their defaults are fixed literals, and help names `$OMNIGLASS_TOKEN` and `$OMNIGLASS_SERVER` as the
sources instead. The [CLI guide](/guides/cli/#connecting) teaches the order and why help is now
safe to paste.

Proven with the token in the environment, not in a flag: a walk of all 390 commands renders each
one's `--help`, `help <command>`, completion of its flag names and of the `--token` value, plus
the four completion scripts, and none carries a value any registered variable supplied
(`TestNoHelpShowsACredential`); a guard fails on any flag whose default, usage or no-value default
carries one, and on any `os.Getenv` in the command package that would dodge that sweep
(`TestNoFlagDefaultReadsTheEnvironment`); the reference renders byte-identically with every
variable unset and set, and the committed page holds nothing token-shaped (`cmd/docsgen`). The
token still authenticates from the environment, a flag still wins over it, in process and against
the built binary and a live server (`TestCLIEndToEnd`).
