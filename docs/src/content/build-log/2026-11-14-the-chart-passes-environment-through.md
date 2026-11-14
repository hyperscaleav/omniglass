---
title: "The chart passes environment through (#1230)"
date: 2026-11-14
refs: [1230, 1287, 1286, 1293, 1295]
---

The Helm chart now exposes `server.extraEnv` and `server.envFrom` so operators can set any registry
variable without editing the chart: `OMNIGLASS_ENCRYPTION_KEY_FILE`, `OMNIGLASS_TRUSTED_PROXIES`,
and any other configuration the chart does not inline. Both values are passed to the server and the
init containers (migrate and bootstrap), so all can reach the same registry. The [helm guide](/guides/helm/#server-environment)
now documents both with examples and names the issue the sentence promised them (#1287). A render test,
the first `helm template` check in the gate, validates the values appear in the manifests.
