---
title: "The server's housekeeping tells its platform component what it did (#1708)"
date: 2026-11-11
refs: [1708, 1094, 1629]
---

The retention pass and the boot's partition check narrated only to the process's stdout. They now
also write `log_line` rows on the server's platform component, next to the figures #1629 lands there:
source `omniglass-server`, facility `retention`, one info line per kind of work a pass did (rows pruned
with the per-lane counts and their total, hours closed into trends, days folded into blocks, partitions
rolled) and a warning carrying the error when a pass fails. The boot writes **metric partitions
created** when it made any. A pass that did nothing writes nothing, and no pass raises an event.

The slog line and the log line are told from one note, so the two cannot say different things. The
write goes through the Storage Gateway under a deadline of its own; a failed write is one warning in
the process's log and the pass goes on, and a server shutting down writes nothing. The planned
`internal_log` table is dropped: the platform component already owns a narration table
([ADR-1708](/architecture/decisions/#adr-1708-the-platforms-self-narration-is-log-lines-on-its-platform-component-not-a-table-of-its-own)).

Integration tests on a real Postgres read the lines back through `ListComponentLogs`: a pass that
prunes 7 aged debug lines leaves a line with `log_line/debug` at 7, a boot that recreates a missing day
partition leaves the creation, and an idle pass writes no line and no event.
