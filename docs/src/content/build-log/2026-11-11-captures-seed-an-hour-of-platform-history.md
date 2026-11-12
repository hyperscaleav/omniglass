---
title: "A capture's lab seeds an hour of platform history (#1790)"
date: 2026-11-11
refs: [1790, 1789, 1267]
---

The docs screenshots of platform health came from a lab a few minutes old, so each server panel's
sparkline was a stub at the right edge of an hour-wide chart. `lab run --platform-history` (hidden,
for scripts) now has the lab's seed write an hour of the server's figures on its platform component
on the lab's first run: a sample a minute, ending the minute before the seed ran, for every key and
instance a server panel reads, source `devseed`. The values are a healthy small lab's, fixed per
series so two captures draw the same lines: failures and queues 0, the pool low against its size.
The database and table sizes and the oldest partition's age climb into what the database measures
as the seed runs, so the server's next live sample carries the line on rather than drawing a cliff.
The docs screenshots pass it.

It is opt-in, and only a throwaway lab may ask: the plan refuses it without `--ephemeral`, and a
scale lab refuses it outright, since its platform figures are its measurement. A learner's lab
writes none. The stream position keys (the consumer's ack floor and the stream's head) are left to
the server: the lab's stream starts over, so a seeded hour would draw a cliff into the live head. So
are the windowed ingest, push and slow query figures: a capture's lab has no node to write them
live, and a windowed figure whose newest sample is two report windows old reads not reported, so a
seeded hour of them would never draw.

A unit test fails any server panel key the history neither seeds, measures nor exempts with a
reason; another holds every failure at 0, and another ends each size on the figure measured. Integration tests on a real Postgres seed through the lab's own
seed one-off: with the flag every series lands sixty samples and a second run adds none; without it
the server carries no sample, and the dev fleet's seed writes none on any platform component.
