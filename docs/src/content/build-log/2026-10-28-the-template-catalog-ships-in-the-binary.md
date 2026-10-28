---
title: "The template catalog ships in the binary and reads over the API (#1323)"
date: 2026-10-28
refs: [1323, 827]
---

A new `internal/template` package holds the catalog: kind-tagged YAML embedded in the binary, in four
kinds (standard, system, place and component templates), loaded and validated without a database and
never written to one. The loader refuses an unknown field, a role naming half a choice, a variant whose
parent the catalog lacks, a component preset that sets the address, a system template that leaves a
role unfilled or answers a choice two ways, and two templates of one name. A build test dry-runs every
shipped template against the seeded catalogs, so a release that withdraws a product, a type, a driver
input, a property or a location type a template still names fails the build, naming both. The shipped
standards are now the standard templates (the seed still plants them from there until the fork ships),
and the templates carry the room metric contract and the layouts the dev seed adds by hand today. `GET
/templates` and `GET /templates/{template}` read the catalog under `template:read` on the viewer floor,
and the CLI and MCP tools are generated from them. The templates page renders the catalog from the
seed-facts artifact, and the ADR-0071 lint narrows so the kind names can be used
([ADR-0827](/architecture/decisions/#adr-0827-the-template-layer-owns-the-clone-model-and-the-componenttemplate-authoring-model-is-superseded-by-the-driver-spec)).
