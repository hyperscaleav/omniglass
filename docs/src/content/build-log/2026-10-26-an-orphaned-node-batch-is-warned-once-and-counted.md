---
title: "An orphaned node batch is warned about once a minute and counted on the node (#1200)"
date: 2026-10-26
refs: [1200, 752]
---

A node batch whose task is not placed on that node (a task moved to another node, or one the server
does not know) is dropped by the confinement fence by design, and the drop left no trace: the
operator saw a component stop updating and nothing said why. The ingest consumer now records it
(`internal/bus/orphan.go`). The first orphaned batch from a node in a minute is a warning naming the
node, the task and the samples dropped; the rest of that minute is only counted, and the next warning
says how many went unlogged, so a node stuck on a moved task cannot flood the log. Every one is
counted on the node: `GET /nodes/{name}` serves `orphaned_batches` and `last_orphaned_at`, written
once a minute per node rather than once a batch, the last of it on shutdown, and a count the store
could not take is kept for the next flush. The tally is pure apart from its lock, so its window,
its count and its forgetting of a quiet node are unit-tested on explicit times; over the real bus,
five orphaned batches inside one window are one warning and a count of five. Migration
`20261026190000_node_orphaned_batches.sql` adds the two columns.
