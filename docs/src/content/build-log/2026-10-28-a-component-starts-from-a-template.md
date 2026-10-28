---
title: "A component starts from a template (#1326)"
date: 2026-10-28
refs: [1326, 827]
---

`POST /components:createFromTemplate` (`omniglass component createFromTemplate`) creates one component from a
component template at a place: its product, the template's declared values, and, when the request's
inputs carry the address and a node, its endpoint attached through the product's driver with the
template's preset under the inputs given. A secret input names a secret the caller may read and is
resolved exactly as an attach resolves it. It is one plan through the planned create, gated by
`component:create` (plus `component:update` for declared values and `endpoint:create` to attach), every
piece in the caller's scope with an audit row carrying `from_template`. A place outside the caller's
location read scope is a non-disclosing 404.
