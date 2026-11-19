---
title: "A terminal demo can stage database state (#1313)"
date: 2026-11-14
refs: [1313, 1289]
---

A tape that declares `# demo-needs: database` now records against a database of its own: the
recorder creates `tape_<id>` on the lab's Postgres, hands the tape `OMNIGLASS_DSN` pointing at it,
and drops it when the tape ends. The recorder image gains `psql`, copied from a Postgres stage
pinned by digest at the lab's major, so a tape can run `omniglass migrate` and stage a row. The
header is parsed in `tapelib.mjs` (an unknown `demo-needs` value is refused naming the line, and the
Docker socket does not combine with a database), and `tapes.sh` reads it from there instead of
grepping the tape. The first such tape, `schema-ahead`, shows a server refusing a schema a newer
release migrated (#1289). Unit tests pin the header's parsing; a `tapes.sh selftest` case records
a fixture that gets its own database (gone after) and one that gets no DSN.
