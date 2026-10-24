---
title: "Tag: the console key directory"
date: 2026-07-13
refs: []
form: bullet
---

- **Tag: the console key directory.** The [tag](/architecture/tags/) vocabulary gets its operator surface: a **Tags**
  directory under Catalog on the shared FlatList shell, where an admin mints a key (`tag:create`), edits its
  governance fields (applies_to and the cascade-versus-flat `propagates` toggle, `tag:update`), and deletes it
  (`tag:delete`, cascading its bindings). Rows address by the key **name** (the write paths key on the name), and the
  create drawer plus the edit blade reuse the estate's drawer and blade primitives, no fork. Binding a value onto an
  entity and the effective-tags cascade panel are the next console slice ([#189](https://github.com/hyperscaleav/omniglass/issues/189)).
  Proven by a data-layer unit test (the envelope unwrap, the create/update/delete request shapes, the error throw)
  and the full web suite, and live-verified against `make dev` (the directory, the create drawer, the edit blade).
