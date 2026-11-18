---
title: "Qualified names pass through the chart's default Traefik ingress (#1138)"
date: 2026-11-13
refs: [1138, 1133]
---

A qualified name rides the URL path with its slash and `$` percent-encoded
(`%2F`, `%24`), and the chart's default ingress class is Traefik, which could have
rejected an encoded slash at the edge. `scripts/test-traefik-encoded-slashes.sh`
runs pinned Traefik containers in front of a backend that echoes the raw request
path and asserts the ingress status equals the direct status with the path intact.
It passes on Traefik 3.0.4, 3.6.25 and 3.7.19 with no entry point options, so the
chart needs no setting; the finding and versions are recorded in the Helm guide.
Setting `allowEncodedSlash: false` on the entry point makes the same check fail
with a 400, so the check does detect a rejecting edge.
