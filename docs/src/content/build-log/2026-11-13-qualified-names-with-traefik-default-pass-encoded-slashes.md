---
title: "Qualified names with Traefik default pass percent-encoded slashes (#1138)"
date: 2026-11-13
refs: [1138, 1133]
---

Qualified names in the operator API address resources by a path containing slashes
(e.g., `campus.room-a.$comp.display-1/https` for an endpoint), with slashes percent-encoded
in the URL path (`/endpoints/campus.room-a.%24comp.display-1%2Fhttps`). The chart's default
Traefik ingress (v3.0) passes these encoded characters through to the backend without
rejection, so qualified names work by default. Documented in the Helm guide that Traefik v3.0
has no restrictions on encoded slashes; if the in-cluster Traefik is upgraded to v3.1 or later,
the operator should verify the entry point's `encodedCharacters` setting permits `%2F` and `%24`.
