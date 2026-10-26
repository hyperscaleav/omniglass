---
title: "The project board tracks the work, moved by one script at fixed points (#1191)"
date: 2026-10-26
refs: [1191]
---

The org project board (#1, "Omniglass") becomes the live status of the work: it had held 27 stale
items against 249 open issues, with status living only in chat. `scripts/board.sh` is the one way an
agent moves it: `add` (Backlog, an existing item's Status kept), `set` (Backlog, Todo, In Progress,
In Review, Done), `iteration <issue> current` and `show`. It resolves the project, its fields, their
options and the issue's item by name once per run, so no skill carries an id; every command is
idempotent; it never writes Priority; and until the board has an In Review status a move there lands
on In Progress with one warning. The skills call it at fixed points: `/define-work` and `/file-bug`
add to Backlog, `/run-feature-loop` sets In Progress as a slice starts, `/ship-slice` sets In Review
as the PR opens, and `/autopilot` makes the same moves and lists them in its day report. Done is
GitHub's built-in workflow, on close; Priority and the iteration are the architect's alone, and
`/file-bug` no longer sets Priority.

[The board](/contributing/labels/#the-board) teaches the fields, who moves what, the four views
(Roadmap, Board, Needs you, In flight) and the one-time setup the API cannot do: the In Review option,
the close and merge workflows, and auto-add from this repository. Proven by `go test ./scripts/`, the
script driven against a fake `gh` that answers from the board's recorded field list and refuses any
id the project lacks (each name resolved to its recorded id, a second run writing nothing, the In
Review fallback warning once, a Priority name refused, the missing scope, issue and project each
named), and once against the live board on #1191, added and then archived.
