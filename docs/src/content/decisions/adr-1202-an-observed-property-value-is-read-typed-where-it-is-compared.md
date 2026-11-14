---
title: "An observed property value is read typed where it is compared"
date: 2026-11-14
status: "Accepted"
summary: "An observed property keeps its text-form encoding; every read that sets it beside a declared or intended value reads it typed by the property's data_type (one SQL expression) and compares with one helper, instead of storing observed properties as typed jsonb"
refs: [1202, 456]
---

- **Date:** 2026-11-14 | **Status:** Accepted | **Pages:** [metrics and properties](/architecture/properties/#reconciliation-want-told-is)
- **Decision:** (#1202.) An observed property row stays the JSON string of its value's text form (the ingest's `propertyText`, written by `to_jsonb(text)`), and a declared or intended row stays typed jsonb. The reads that set the two beside each other (reconciliation's `is`, settlement's observed value, the calculated arm of the effective properties) read the observed value through one SQL expression, `observedValueExpr`, keyed on the property type's `data_type`: a `bool`'s `"true"`/`"false"` becomes the boolean, a `json` value's text becomes the JSON it spells, a `string` is left as stored. Reconciliation's `worstCaseValue`, settlement's `agreeingValue` and `Settle` compare through one helper, `sameValue`, which also settles spelling (`3` and `3.0`, an object's key order).
- **Consequences:** A `bool` declared `true` and observed `true` report no drift and `is` reads `true`; a command targeting it settles on the observation. A string property is compared as text, so a version `"1.10"` never equals `"1.1"`. The transition-only guard is untouched, since the stored encoding is. One ambiguity remains, inherited from the text encoding: a `json` property observed as the JSON string `"3"` reads back as the number `3`. The seeded property types are all strings, so none is affected.
- **Context:** The issue offered two directions: store observed properties as typed jsonb, or compare canonical forms keyed on `data_type`. The calculated arm of the effective properties already read a bool this way (the partial canonicalization the issue names); this extends it to the other comparison sites and to `json` values, and puts it in one place.
- **Not chosen:**
  - **Store observed properties as typed jsonb.** It removes the ambiguity, but the text form is what every `value #>> '{}'` reader (health, fleet and platform views, endpoints), the calc inputs, the rule engine's transition guard and the reply observation write assume, and each writer (the node lane, push, calc output, a command's reply, the dev seed) hands the gateway text. It is a wire and storage change across all of them for a defect in two comparisons, and it can be taken later without undoing this: the read expression is the identity on a typed value.
