---
title: "The chart passes environment through (#1230)"
date: 2026-11-14
refs: [1230, 1287, 1286, 1293, 1295]
---

The Helm chart now exposes `server.extraEnv` and `server.envFrom` so operators can set any registry
variable without editing the chart: `OMNIGLASS_TRUSTED_PROXIES` and any other configuration the
chart does not manage directly. Both values are passed to the server, the migrate init container,
and the bootstrap init container when `bootstrap.enabled` is true, so the Omniglass containers share one configuration. The
[helm guide](/guides/helm/#server-environment) now documents both values with examples, replacing the
previous vague "extraEnv-style values or a Secret" wording with named chart values. A render test
using the Helm Go library validates the values reach the server, migrate and bootstrap containers, with and without bootstrap.
