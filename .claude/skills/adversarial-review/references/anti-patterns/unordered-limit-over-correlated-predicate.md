## unordered-limit-over-correlated-predicate (fixed; found by the #417 review)

- **Cue:** a chunked `delete ... where id in (select ... where <a> and <b> limit $n)` with no
  `ORDER BY`, over predicates a workload makes correlated (retention keeps old rows of one severity
  rarer than the planner's independence assumption predicts), held only by a plan test run with
  sequential scans priced out.
- **Failure:** with no order to satisfy, the planner prices a sequential scan that stops at the
  LIMIT, expecting the matches early. A chunk that finds few or none reads the whole table. The log
  prune read 2.4M rows for an empty chunk. A small table's planner also hash-joins the outer
  `IN` over a sequential scan instead of probing the primary key. Neither shows with
  `enable_seqscan` off, which reaches the index either way.
- **Fix:** order the subquery as the index it should walk (`order by c.severity, c.ts`, or
  `order by c.ts`), so the LIMIT stops inside the index. Take the chosen ids as an array
  (`where id = any(array(select ...))`) so the outer delete probes the primary key. Pin both with a
  statement-shape test, since a plan test cannot see them
  (`internal/storage/prune_sql_internal_test.go`, `TestPruneStatementsKeepTheirShape`).
