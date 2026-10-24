---
title: "settings are a reflected typed struct with generated client and server validation"
date: 2026-07-19
status: "Accepted"
summary: "A setting is declared **once**, as a tagged field on a canonical `Settings` Go struct; reflection produces the `code` defaults layer and the namespace registry, Huma reflects the struct into the OpenAPI schema so the typed client `values` is a `Settings` struct, and both the server PATCH and the generated client validate against that **same reflected schema**. Closes the slice-0 write-validation thin cut; retires the hand-kept `defaults.yaml` and `Namespaces()` slice"
---

- **Date:** 2026-07-19 | **Status:** Accepted | **Pages:** [settings](/architecture/settings/)
- **Decision:** A setting is declared **once**, as a tagged field on a canonical `Settings` Go struct
  (`internal/settings/schema.go`), and that single declaration is the whole source of truth. Reflection over the
  struct produces the `code` defaults layer (`Defaults()`, from each leaf's `default:` tag) and the namespace
  registry (`Namespaces()`, from the `json` and `settings:` tags), replacing the hand-kept embedded
  `defaults.yaml` and the hand-kept `Namespaces()` slice (both retired). Huma reflects the struct into the
  OpenAPI schema, so `make gen` yields the typed SPA client `values` (a `Settings` struct, not a free-form
  object). Writes validate against that **same reflected schema** on both sides: the server backstops
  `PATCH /settings/{namespace}` (unknown namespace to 404, unknown key / wrong type / `enum` or `pattern`
  violation to a 422 naming the `namespace.key`, `null` allowed as a delete), and a `make gen` step slices the
  field constraints out of `api/openapi.json` into a committed client artifact
  (`web/src/api/settings.schema.gen.ts`) that drives inline form validation (enum-as-select, Save blocked while
  invalid). The cascade merges partial generic maps as before; typing lives only at the edges (the effective
  read unmarshals into `Settings`, and Go code reads a setting through the `EffectiveTyped` accessor).
- **Context:** Slice-0 shipped the engine with **untyped** values: a setting lived in two hand-kept places (the
  `Namespaces()` slice and the embedded `defaults.yaml`), the API exposed `values` as a free-form object, the
  generated client typed it as `Record<string, unknown>`, and the PATCH write accepted any namespace, key, or
  value and stored it as-is (the documented write-validation thin cut). That is the one surface that dodged
  doctrine 1 (API-first, typed, generated). Making `Settings` a reflected struct pulls the default, the schema,
  the typed client, both validators, and the typed accessor from a single declaration, so adding a setting is
  one tagged field and there is no second place to drift. The cascade keeps merging partial maps because a Go
  struct cannot express "unset" versus a zero value; typing is applied only at the edges. This **closes the
  slice-0 write-validation thin cut** and retires the `defaults.yaml` asset and the `Namespaces()` list.
- **Deferred:** the declarative operator-file machinery (a generated JSONSchema for the operator `settings.json`,
  validation of the **file** layer at boot, and letting the file layer take precedence over the database, the
  GitOps-wins / read-only lever) is a future slice on the same epic, as are operator-open namespaces (a typed
  map with a `Default()` method) and the group and user cascade rungs; none is built here.
- **Closes:** issue [#288](https://github.com/hyperscaleav/omniglass/issues/288) (settings engine slice-1), under
  epic [#270](https://github.com/hyperscaleav/omniglass/issues/270).
