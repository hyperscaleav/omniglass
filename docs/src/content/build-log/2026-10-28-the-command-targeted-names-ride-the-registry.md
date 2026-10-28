---
title: "The command-targeted property names ride the kept registry (#1299)"
date: 2026-10-28
refs: [1299, 1265, 1279]
---

Settlement only looks at observations of a property some command type targets, and every batch
carrying a property sample read that set from `command_type` inside its transaction, though it
changes only when a command type does. The ingest consumer now keeps the set beside the registry it
already keeps between batches, rebuilt when the catalog fingerprint moves, which now covers command
types too, and hands it to each batch
([ADR-1299](/architecture/decisions/#adr-1299-the-command-targeted-property-names-ride-the-kept-registry)).

A node batch costs 18 statements instead of 19, with no new read. A command type created between two
batches is honored by the second: a test creates one, issues a command, and the next batch's
observation settles it.
