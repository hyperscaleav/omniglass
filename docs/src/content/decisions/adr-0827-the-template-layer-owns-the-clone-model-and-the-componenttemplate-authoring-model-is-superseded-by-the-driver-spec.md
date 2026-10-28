---
title: "The template layer owns the clone model, and the ComponentTemplate authoring model is superseded by the driver spec"
date: 2026-10-28
status: Accepted
summary: "#827 owns only ADR-0071's clone model: standard, component, system and place templates shipped in the binary, read over `GET /templates` (`template:read`) and created through `POST /standards:fork` and `:instantiate` custom methods gated by each resource's own create permission and scope, provenance in the audit detail only. The ComponentTemplate collection-authoring model (functions, a step DAG, `inputs`, `:apply`) is superseded by the driver spec; its remainders move to #1331 and #318, the registry scope ladder's template rung retires, and `:apply` retires."
refs: [827, 1322, 1331, 318, 1330]
---

- **Date:** 2026-10-28 | **Status:** Accepted | **Pages:** [templates](/architecture/templates/), [data collection](/architecture/collection/), [glossary](/architecture/glossary/), [expressions](/architecture/expressions/), [nodes](/architecture/nodes/), [metrics and properties](/architecture/properties/#key-scope-org-and-official), [cascade](/architecture/cascade/), [config and variables](/architecture/variables/), [the spine](/architecture/), [roadmap](/architecture/roadmap/), [identity and access](/architecture/identity-access/), [health](/architecture/health/), [API](/architecture/api/)
- **Decision:** #827 owns the clone model of
  [ADR-0071](#adr-0071-a-template-is-a-clonable-example-not-a-versioned-shape-an-instance-pins) and
  nothing else. Four kinds (`standard_template`, `component_template`, `system_template`,
  `place_template`) ship as kind-tagged YAML embedded in the binary, read through `GET /templates`
  under a new `template:read` on the viewer floor, and are created from through custom methods on
  their target collections: `POST /standards:fork` (`standard:create`) and
  `POST /{components,systems,locations}:instantiate`, each gated by that resource's create
  permission, every piece checked in the caller's own scope and audited in one transaction. The
  template's name is recorded in each created row's audit detail and nowhere else (no column, no
  back-pointer). There is no template versioning, and only shipped templates exist (operator-authored
  templates are #1330). The stand-in standards boot seed retires once the catalog ships, the
  component template is in, and the work lands as two PRs. The `ComponentTemplate` collection-authoring
  model the collection, glossary, expressions and nodes pages fenced under #827 (a versioned
  template's `inputs` bound at `:apply`, functions as a trigger plus a step DAG, Expr extractors and
  step guards) is **superseded by the driver spec**
  ([ADR-0135](#adr-0135-a-drivers-spec-is-data-one-engine-interprets-it)); those fences become
  historical records. Its still-wanted remainders (an Expr transform on an extractor, a step guard,
  shared Omniglass functions, function-run bindings) move to #1331, and the catalog's community and
  private tiers with the template trust surface (signature, capability manifest) to #318. The
  registry scope ladder's **template** rung retires: a template is never a row, so nothing can be
  scoped to one, and the ladder is org and official (#318). `:apply` retires: the node re-pulls its
  whole worklist every tick, an endpoint edit or a driver spec change re-derives its tasks in the
  same transaction (#823, #1061), and the worklist's `config_generation` is built, so nothing is left
  for an apply step to do. The cascade's two "template bands" are the product and standard
  classifier bands (#1255, #184), and a signal is settable when its driver binds a command for it,
  not when a template binds a set function. The kind names `component_template` and
  `system_template` are reclaimed for the catalog: the ADR-0071 vocabulary lint narrows to the
  versioned-shape tables it retired (`component_template_version`, `system_template_version`,
  `system_template_member`).
- **Context:** The definition of #827 found that every fence citing it for collection authoring
  described a model the driver spec had already replaced in the tree: typed inputs, poll functions,
  listeners and command bindings, attached per endpoint, re-derived on change. Keeping the step DAG
  and `inputs` under #827 would have built a second authoring model beside the driver spec, and the
  template rung of the scope ladder presupposed a template row that ADR-0071 already ruled out. The
  architect approved the definition with every open decision resolved as recommended; this entry
  records those resolutions so the pages can say what is true now.
