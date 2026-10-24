---
title: "Platform: typed settings (slice 1)"
date: 2026-07-19
refs: []
form: bullet
---

- **Platform: typed settings (slice 1).** A setting is now declared once as a field on a canonical **`Settings`**
  Go struct: reflection over its `default`, `enum`, `pattern`, and `settings:"<domain>,<visibility>"` tags builds
  the code-defaults layer and the namespace registry (the embedded `defaults.yaml` and the hand-kept
  `Namespaces()` list are retired), so adding a setting is one tagged field with no second place to drift
  ([ADR-0041](/architecture/decisions/#adr-0041-settings-are-a-reflected-typed-struct-with-generated-client-and-server-validation)).
  The cascade still merges partial maps, but the effective read unmarshals into the typed struct: the API `values`
  is now the typed **`Settings`** (the generated client gets `values.ui.theme` as a union), and Go code reads a
  setting off `settingsSvc.EffectiveTyped(ctx)`. Writes validate against the reflected schema (Huma
  `SchemaFromType` plus `Validate`): an unknown namespace is a **404**, an unknown key or a value failing its
  `enum`/`pattern`/type is a **422**. A `make gen` step slices those field constraints out of the OpenAPI into
  **`web/src/api/settings.schema.gen.ts`**, and the settings form validates each field **inline** against it
  (an enum renders as a select of the generated options, a bad value shows an inline message and blocks Save),
  with the server 422 as the backstop, so the console and the server enforce the same rules from one source.
  `default_landing` gains a `^/` pattern to make the inline validation demonstrable. Proven by the reflection
  unit suite (defaults coercion, registry from tags), the validator suite (unknown-namespace, unknown-key, enum
  and pattern violations, null-delete skipped), HTTP e2e (a bad `PATCH` is 422 and leaves no override, an unknown
  namespace is 404), and web tests (the inline validator plus the form rendering the generated enum). Deferred:
  the declarative operator-file machinery (a generated schema for the operator file, file-layer validation, and
  letting the file layer outrank the database at the platform level).
