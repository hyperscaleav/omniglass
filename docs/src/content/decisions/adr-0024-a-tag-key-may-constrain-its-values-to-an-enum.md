---
title: "a tag key may constrain its values to an enum"
date: 2026-07-13
status: "Accepted"
summary: "A tag key may declare an `allowed_values` enum (empty = free text), enforced on the binding write; a free key autocompletes its distinct in-use values"
---

- **Date:** 2026-07-13 | **Status:** Accepted | **Pages:** [tags](/architecture/tags/)
- **Decision:** A tag key gains an **`allowed_values`** set (a new `text[]` column, empty by default). Empty leaves
  the key **free-text**, unchanged; a non-empty set is the **enum** a bound value must belong to, so `environment`
  can be declared as one of `prod`, `staging`, `dev`. The **binding write enforces it**: `SetTagBinding` rejects a
  value outside a key's non-empty allowed set with a dedicated 422 (`ErrTagValueNotAllowed`), so the constraint is a
  real server gate, not a UI hint. The Tags directory create and edit forms carry a value-domain control (a checkbox
  that turns the key into an enum plus a value-list editor), and the TagAdder value stage renders a **strict dropdown**
  for an enum key. A **free** key instead offers **value autocomplete from the distinct values already bound** for it,
  through a new `GET /tags/{name}:values` read (a `select distinct value`), so an operator reaches for an existing
  value without the key having to declare a set up front. Only the enum (a string set) ships; a typed `value_type`
  (int, bool, date) and input normalization (lowercase, trim, fold) stay the page's open question.
- **Context:** The [tags](/architecture/tags/) page left value-domain governance an open question, with the enum, a
  typed value_type, and normalization all on the table. Operators asked first for the plain case, a key like
  `environment` that should only ever be one of a short list, so that shipped: a string enum on the key, enforced on
  write, with a strict picker. The distinct-in-use autocomplete is the free-key counterpart, cheap (one `select
  distinct`) and immediately useful, so the two ship together. This resolves the enum half of the page's open
  question; the value_type and normalization halves remain deferred.
- **Closes:** issue [#190](https://github.com/hyperscaleav/omniglass/issues/190) (tag value-domain governance, enum).
