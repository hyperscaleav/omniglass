---
title: "Tag: the governed label vocabulary and its cascaded bindings"
date: 2026-07-13
refs: []
form: bullet
---

- **Tag: the governed label vocabulary and its cascaded bindings.** The [tag](/architecture/tags/) primitive lands as
  its own page: a governed **`tag`** key registry (a normalized lowercase-identifier vocabulary, minting gated by
  all-scope `tag:create`, broadened to `tag:*` for admin), a per-entity **`tag_binding`** cell owned on the same
  exclusive arc as a secret and a variable (`platform | location | system | component`), and a resolver that **unions
  keys and overrides values** most-specific-wins down the [cascade](/architecture/cascade/). The governance split is the
  point: minting a key is admin-curated, but **setting a value is the entity's own write** (`component:update` and
  friends), so an operator tags what it may already edit with no new grant; a platform-tier binding is `tag:update`
  plus `platform:update`. A key
  carries **`applies_to`** (an entity-kind allow-list, checked on bind) and **`propagates`** (cascade inheritance
  versus a flat per-entity set, the shape a [file](/architecture/files/) reuses). `GET|POST /tags`,
  `PATCH|DELETE /tags/{name}`, `POST /tags/{name}:setPlatform|:clearPlatform` (the platform-tier value), and per-entity custom
  methods `GET /{components,systems,locations}/{name}:listTags` and `POST .../{name}:setTag|:removeTag` (bindings are
  entity custom methods, like the principal lifecycle, so the generated CLI stays collision-free), plus
  `GET /components/{name}/effective-tags` (the per-component cascade). Deferred (ADR-0021): the console surface (a Tags
  directory and per-entity editor, [#189](https://github.com/hyperscaleav/omniglass/issues/189)), binding via
  [groups](/architecture/groups/) and a `template`-scoped binding ([#184](https://github.com/hyperscaleav/omniglass/issues/184)),
  value-domain governance ([#190](https://github.com/hyperscaleav/omniglass/issues/190)), and binding onto a file
  ([#191](https://github.com/hyperscaleav/omniglass/issues/191)). Proven by a pure `internal/tag` validation unit test (key
  normalization, value bounds, the applies_to allow-list), a storage integration test (the registry CRUD and all-scope
  gate, the applies_to and value gates, the binding upsert and the owner-update scope split, the full union-on-key /
  override-on-value cascade incl. the non-propagating flat case, and the delete-key cascade to bindings), and an HTTP
  e2e (the cascade over the wire and the mint-versus-bind authz split: an operator binds on its component but cannot
  mint a key nor bind on a system it cannot write, a viewer reads but cannot mint or bind).
