---
title: "The roles table stops being a second copy of the seed"
date: 2026-08-13
refs: []
form: bullet
---

- **The roles table stops being a second copy of the seed**
  ([#722](https://github.com/hyperscaleav/omniglass/issues/722)). [Identity and access](/architecture/identity-access/)
  carried a hand-written matrix of the five official roles and what each can do, beside an ASCII
  inheritance diagram, while `seed.json` already renders the same roles with their declared and
  effective permissions resolved through the authorizer's own `rbac` path.
  [#714](https://github.com/hyperscaleav/omniglass/issues/714) had already had to correct one stale
  claim in two places because of it.

  Two more had accumulated, both found by reading `roles.yaml` rather than the page. The `operator`
  row claimed "ack/snooze/resolve alarms", a capability the role holds no permission for and the API
  publishes no route for, while omitting `telemetry:push`, `command:issue`, `file:create,delete` and
  the four catalog grants it does hold. The diagram drew `admin <- owner`, and `owner` inherits
  nothing at all: its permission is `>`, so there is nothing an edge could add. Neither was written
  wrong; both were written once and then left behind by the seed, which is the entire argument for
  rendering.

  The table and the diagram are now one `SeededSet` render, and what the permission strings cannot say
  stays hand-written beside it, per role: why the IAM directories are out of `viewer`'s reach, why an
  admin-sensitive secret is a 404 rather than a 403, why a `deploy` grant reaches exactly one tier, why
  `admin` is not the superuser. That split is the rule the page now follows, and it is the same one
  [the access guide](/guides/admin/access/) already followed.
