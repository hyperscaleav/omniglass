---
title: "Samples are pruned to their retention window (#417)"
date: 2026-10-28
refs: [417, 1265]
---

Nothing pruned the sample tables, so the metric table grew by millions of rows a day at the lab's
cadence and never shrank. The server now keeps metric samples for 14 days and property transitions
for 90, install-wide windows an admin changes in the new **retention** settings
([ADR-0417](/architecture/decisions/#adr-0417-the-server-prunes-each-sample-lane-to-a-window-in-bounded-chunks)).
Every hour a pass removes what is older in chunks of 5,000 rows, each a short statement of its own,
so a first pass over a large backlog never holds the database. While a lane is behind, the next pass
comes a minute later.

The floor holds. A declared value and the latest row of every series are kept however old. "Latest"
holds under both orders the readers use, so a clock that stepped back costs nothing. A command's
own rows are kept too, for its history.

The old `PruneSamples` ranked every row of both tables on every call. The new one applies the cutoff
first, then checks each old row for a newer row of its series with an index search. A new partial
index covers the one owner the existing indexes missed, a metric a system or a location owns. The
review caught an `EXISTS` form of that check that Postgres turned into a hash over the whole table,
and an unbounded window that overflowed the timestamp encoding; the check is now a limited
subquery, held per owner by the accesspath instrument, and windows stop at a century. It also
caught the console's Settings page sending an integer as text, which this first integer setting
would have hit on every save.

Log lines and a window per type are the next two slices of #417.
