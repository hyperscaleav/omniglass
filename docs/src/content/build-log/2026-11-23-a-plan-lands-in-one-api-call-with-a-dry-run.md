---
title: "A plan of locations, systems and components lands in one API call, with a dry run (#2368)"
date: 2026-11-23
refs: [2368, 827, 522]
---

`POST /plans:apply` (`omniglass plan apply`, an MCP tool) exposes the planned-create primitive,
`storage.CreatePlan`, to a client: standard templates, locations with parents, systems with their
slot-filling components, standalone components and declared values land in one transaction under
the caller's scope, each piece the ordinary create with its own audit row. `validate_only` runs the
same transaction and rolls it back, answering with the counts and the names an apply would mint. An
apply stamps one import id into every audit row as `from_import` (the new `storage.WithImport`,
beside `WithTemplate`) and returns it. A refused piece names its path in the plan
(`systems[3].components[1]`), and a plan over 1000 rows is a 413 naming the limit, so a client
chunks by building. The integration test dry-runs a 500-row plan, applies it, and finds every audit
row under its import id. The decision is [ADR-2368](/architecture/decisions/#adr-2368-a-client-applies-a-plan-in-one-call-under-one-import-id).
