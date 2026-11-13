---
title: "Lab measure samples a running lab into a time series and times the system page (#1632)"
date: 2026-11-09
refs: [1632, 1623]
---

`omniglass lab measure --every 10m --append scale.jsonl` now samples a running scale lab on an
interval: the lab is found and warmed up once, then each interval measures one window and appends one
JSON line with the one-shot report's fields at the same paths `--json` writes them, plus `seq`, `at`
and the server's uptime, so a series measured across a restart shows it. A sample that fails writes a
line with its error and no figures, and the run goes on. Each line, and each one-shot report, now
carries the database's absolute levels as well: its size, the metric partitions and the oldest day,
the rollup tables' bytes and the dead tuples vacuum has not reclaimed, the figures whose trend says
whether retention and vacuum keep up. `--page-clients N` times the console's system page the way an
operator waits for it, every read a cold open makes issued at once, under N concurrent clients, and
reports its p50 and p95 with a load error ratio. The read list is generated into a fixture the page's
own test compares against what the page fetches, so the load cannot drift from the page. The guide's
hand flag table is gone in favour of the generated CLI reference.
