---
title: "A template is cloned once, a standard is built to, and the label string is a rule"
date: 2026-11-16
status: "Accepted"
summary: "Template keeps one meaning, the catalog entry the binary ships and an operator clones once; standard keeps its meaning, the operator's live contract a system is built to; the Go text/template string a label or notification renders from is a rule, never a template"
refs: [2033, 2028, 2065, 2034]
---

- **Date:** 2026-11-16 | **Status:** Accepted | **Pages:** [glossary](/architecture/glossary/),
  [templates](/architecture/templates/), [storage](/architecture/storage/)
- **Decision:** (#2033.) Both words stay, and each keeps one relationship. A **template** is an
  entry in the catalog the binary ships (`/templates`, the four kinds `standard_template`,
  `system_template`, `location_template`, `component_template`): creating from one writes ordinary
  rows the operator owns, and nothing points back at it. A **standard** is the operator's own row
  and a live contract: the systems built to it stay bound, and its slots, metrics and properties
  keep judging them. The question "which do I edit" always answers the standard, since a template
  is read-only. Prose calls a standard the contract a system is built to, never a blueprint, which
  explained one word with a third. The other sense of template, the Go `text/template` string a
  label or a notification renders from, loses the word: it is a **rule**, as the API already says
  (`label_rule` on every table and wire field), and notification `subject` and `text` are written
  in the rule language. The `label_rule` columns `default_template` and `template` become
  `default_rule` and `rule`; "label template" joins the docs denylist; naming the engine as Go
  `text/template` stays. The renames are #2065.
- **Context:** the issue asked whether the shipped starting point and the operator's copy need
  separate words. They are different relationships (a snapshot copied once against a contract
  systems conform to), so they do, and the existing pair already maps onto them;
  `standard_template` reads head-noun-last as a template that produces a standard. Standard is the
  word enterprise AV teams already use for exactly this (an AV or room standard: the build, its
  bill of materials, what conforms), with its verbs (built to standard, out of standard), and the
  glossary's own framing, the system-side counterpart of `product`, holds. It stays bare rather
  than owner-qualified: a component-side standard would be a product. Considered for standard:
  **spec** (the OpenAPI spec, a ticket's Spec section, and a document rather than a contract),
  **profile** (CLI profiles), **design** (a one-off per project, where a standard is reused),
  **class** (a seeded system type), **model** (product model numbers), **blueprint** (what you
  build from once, which is a template), **baseline** (the perf baseline) and **reference design**
  (vendor-authored). Considered for the label string: **pattern**, which would add a second noun
  beside the `label_rule` already on the wire. The overlap a standard does have, with **system
  type** (both nest, both classify systems), is #2034's to settle.
