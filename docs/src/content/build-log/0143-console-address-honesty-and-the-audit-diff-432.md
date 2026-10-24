---
title: "Console address honesty and the audit diff ([#432](https://github.com/hyperscaleav/omniglass/issues/432))"
date: 2026-08-01
refs: [432]
form: bullet
---

- **Console address honesty and the audit diff
  ([#432](https://github.com/hyperscaleav/omniglass/issues/432)).** The console now addresses everything the way
  [ADR-0062](/architecture/decisions/) promised: the uuid is identity, the kebab `name` is what an operator reads,
  types, and posts. Nine slices: the location_type and secret_type pickers post the handle the write paths resolve
  (a fresh install could not add a location from the console before); every registry write accepts either form
  through `registryRefCol`; the six catalog pages (Drivers, Vendors, Products, Capabilities, Standards, Types) lead
  with a Name column, filter and address rows by the handle, and keep the uuid as a dim blade fact; Products
  renders vendor/driver handles and its capability picker joins on the names the wire carries, so removal works; a
  successful create now lands the operator on the new row's blade across all twelve catalog pages; the 52 blank
  CLI-reference flag descriptions are filled at their Huma `doc:` tags and a docslint check keeps the class from
  recurring; and the audit read finally answers "and to what?": `old`/`new` ride the wire and the Audit page's
  first detail surface renders a field-level before/after diff (a pure `diffFields` projection), with redaction
  swept and pinned by an e2e (a secret's audit body carries metadata only). The prevention half is a
  fixture guard: console test fixtures must mint uuid-shaped registry ids (`uuidFor`), and converting 25 files
  flushed out four more live uuid-vs-name joins (the Systems standard picker, RoleEditor, AlarmsPanel,
  CapabilitiesPanel), fixed in the same sweep. The `uuid-as-address` anti-pattern is retired in the adversarial
  catalog. Run as the third feature loop on the ADR-0074 machinery.
