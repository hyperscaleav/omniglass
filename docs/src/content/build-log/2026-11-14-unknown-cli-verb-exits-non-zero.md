---
title: "An unknown CLI verb under a group now exits non-zero (#1311)"
date: 2026-11-14
refs: [1311, 961]
---

`omniglass component alarm raise x` printed the alarm help and exited 0, so a pipeline step that
guessed a verb passed green while nothing happened. Cobra checks for an unknown subcommand only on the
root, and validates `Args` only on a runnable command, so a bare `Args: cobra.NoArgs` on a group does
nothing. One walk over the assembled tree in `internal/cli/root.go` now gives every pure group (children,
no run of its own, generated or hand-written) `Args: cobra.NoArgs` and a RunE that prints help, so a bare
group still exits 0 and an unknown word is `unknown command "raise" for "omniglass component alarm"`.
The group is marked by an annotation so the CLI reference generator and the docs-invocation check still
treat it as a group. Tests cover the two repro invocations, a bare group still printing help, and a tree
walk that fails if any group accepts an unknown positional.
