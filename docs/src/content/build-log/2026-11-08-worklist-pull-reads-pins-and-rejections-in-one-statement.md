---
title: "A node's worklist pull reads key pins and rejections in one statement each (#1571)"
date: 2026-11-08
refs: [1571, 1556, 1568]
---

The 1,000-system corporate scale lab never came up: its headquarters node (806 components, 4,434
tasks) timed out on its first worklist pull (the node waits 5 s) and restarted in a loop while the
other 17 sites came up. The cause is product code, not lab code. `NodeWorklist` read a key pin per ssh
or tls endpoint (`keyPin`), a rejection per component endpoint (`latestRejection`), and parsed the
driver spec once per task, so the headquarters pull issued about 4,330 statements: 1.15 s on an idle
database; with 18 nodes pulling through the server's one worklist subscription it did not finish inside 5 s.

The pull now reads every pin in one statement (`keyPins`) and every rejection in one (`latestRejections`),
and parses each driver spec once per pull. `keyPin` and `latestRejection` stay as one-element calls of the
batch, so there is one implementation of each read. The headquarters pull is 1,106 statements and 0.43 s,
and all 18 sites' pulls together cost 1.26 s of statements, down from about 7.5 s. A statement-count
test (`TestNodeWorklistStatementsDoNotGrowWithEndpoints`) failed before the change (21 statements for 9
endpoints, 7 for 2) and passes after it. `TestPlanWorklistMeasure` is the probe behind these figures.

The 1,000-system bring-up still did not reach ready after this change: the headquarters node's pull
still timed out ("pull worklist: nats: timeout", 17 restarts in 10 minutes) while the other 17 sites
came up by 168 s and the server and database ran at 50 to 290% and 50 to 420% CPU. The server logs no
per-pull timing, so the logs show the timeout but not how long the pull took under that load. Pulls
that are answered one at a time, and abandoned ones still computed, are #1577. The 1,000-system column
of the scale measurement is not recorded yet.
