---
title: "a template is a clonable example, not a versioned shape an instance pins"
date: 2026-07-31
status: "Accepted, amended by ADR-0827"
summary: "A template is an example configuration an operator **clones**: creating from one is a one-time fork with no inheritance and no back-pointer, so templates stay **upgrade-safe because nothing remains connected to them**. The versioned-shape model retires (`component_template`, `system_template`, `*_version`, channels, the frozen BOM, instance pinning); a component's shape is its `product`, a system's is its `standard`, and a system **conforms** to that standard with live inheritance. Reverses ADR-0045's deferral and ADR-0049's \"templates stay `Design`\""
---

- **Date:** 2026-07-31 | **Status:** Accepted | **Pages:** [templates](/architecture/templates/), [core entities](/architecture/core-entities/), [cascade](/architecture/cascade/), [glossary](/architecture/glossary/), [collection](/architecture/collection/)
- **Decision:** A **template is an example configuration an operator clones**. Creating from one is a **one-time
  fork with no inheritance and no back-pointer**, so a template can be improved in any release without migrating
  anyone: **it is upgrade-safe precisely because nothing stays connected to it**. What an operator instantiates from
  a template is an ordinary row they then own, whether that is a `location_type`, a `standard`, or a whole system.
  The **versioned-shape model is retired**: `component_template`, `system_template`, their `*_version` rows, the
  `stable` / `beta` channels, the frozen BOM, and "an instance pins a version" or "tracks latest" are gone. A
  component's shape is its **`product`** (with the `product_property` contract), and a system's shape is its
  **`standard`**, to which a system **conforms with live inheritance**. Forking applies template to row; conformance
  applies row to instance. The word *template* survives; its meaning is inverted.
- **Context:** The decision log and the code had drifted into disagreement, and neither said so. ADR-0045 deferred
  "a product's own template or field-schema binding" and ADR-0049 stated "Templates and their frozen BOM stay
  `Design`; the two models are reconciled when template pinning is built", so the log asserted the pinned model was
  merely *deferred*. Meanwhile ADR-0047 retired `component_type`, ADR-0048 promoted `system_type` to `standard`, and
  the shipped schema pointed a component at `product_id` and a system at `standard_id`. A 2026-07-30 vocabulary
  audit found `templates.md` still teaching version pinning across 29 lines, with the retired nouns reaching search
  results through the page's own frontmatter description, and no denylist entry anywhere because the estate-model
  ADRs had each introduced a new noun without retiring the old one.
  The two models are not variants of one idea, they are opposites. The pin existed **so a template could not change
  under an instance**; the fork exists **so a template can change freely because no instance is watching**. Holding
  both was what made the docs unresolvable: every page had to hedge. This generalizes the fork-seed model ADR-0048
  already shipped for standards and location types (tracked for build in
  [#317](https://github.com/hyperscaleav/omniglass/issues/317)) and extends it to instantiating a **system** from a
  template, which is the operator-facing half. It also removes two rungs from the structural cascade: a template
  cannot be a resolution tier when nothing points at it.
  Rejected: keeping pinning for components while forking systems (two mental models for one word), and retiring the
  word *template* entirely (it is the right word for a clonable example, and the fork model is what an operator
  already expects from "start from a template").
