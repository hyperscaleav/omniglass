---
title: "A system starts from a template, whole (#1327)"
date: 2026-10-28
refs: [1327, 827]
---

`POST /systems:createFromTemplate` (`omniglass system createFromTemplate`) lands a whole system from a system
template at a place in one transaction: its standard (the caller's of that name, else created from the
standard template with any ancestor the fleet lacks, parents first), a component per role filled to its
quorum by the shared planner with the product the template names, each assigned so the staffing reads
complete, and the declared values. A role given an address in `attach` lands attached; the rest are
listed in `to_attach`. The route checks every permission its pieces need up front, and a refused piece
(a component outside the caller's create scope, a standard to create from its template without `standard:create`) lands nothing.
