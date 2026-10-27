---
title: "A lab says it is one, and lab and real databases refuse each other (#1195)"
date: 2026-10-27
refs: [1195, 1193]
---

The lab marks the database it creates (the `lab_mark` table, whose one row the lab's seed writes;
never a migration or boot-seed row) and starts its server in lab mode, a hidden setting only
`omniglass lab run` passes. A server outside lab mode refuses a marked database, and a lab refuses a
database that holds data and no mark, each saying why and what to do. `GET /auth/status` reports
`lab`, and the console shows a **Lab instance** banner on the sign-in page and in the shell. The
[lab guide's safety rails](/guides/lab/#safety-rails) teach it.
