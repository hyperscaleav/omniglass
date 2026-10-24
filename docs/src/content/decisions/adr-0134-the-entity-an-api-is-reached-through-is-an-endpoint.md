---
title: "The entity an API is reached through is an endpoint"
date: 2026-08-28
status: "Accepted"
summary: "The entity formerly named `interface` renames to `endpoint` end to end (table, routes, `endpoint:*` permission nouns, console, docs, the `endpoint-reachable` datapoint renamed in place), executing #603's naming ruling as ADR-0073's transports-become-code lands: `service` was disqualified by the existing service-principal table, `api` by the platform's own API vocabulary, and `endpoint` is the word the design already used for this thing's address, which becomes `address` so the schema never reads `endpoint.endpoint`"
---

- **Date:** 2026-08-28 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/),
  [nodes](/architecture/nodes/), [glossary](/architecture/glossary/), [API](/architecture/api/),
  [identity and access](/architecture/identity-access/)
- **Decision:** The entity formerly named `interface` is an **endpoint**, renamed end to end in one
  slice (#811, the naming ruling on #603): the table (with the task arc's column and every
  constraint code names), the routes (`/endpoints`, plus `GET /transports` serving the code
  registry), the permission nouns (`endpoint:*`, with existing role arrays rewritten in place), the
  console surfaces and gate words, the docs corpus, and the canonical `interface-reachable`
  datapoint, whose registry row renames in place to `endpoint-reachable` so every uuid-keyed sample
  keeps its history. The designed driver-spec field that used to be called `endpoint` (the probed
  `host[:port]`) is `address` from here on, so the schema never reads `endpoint.endpoint`; the
  reachability read already serves it as `address`. `interface` and `interface_type` join the
  docslint denylist; the decision log and build log keep their historical spellings.
- **Context:** The word had to move before the collection epic multiplied its vocabulary (sessions,
  listeners, and command bindings all hang off this entity), and slice 1 was the lifetime-cheapest
  carrier since ADR-0073's transports-become-code was already reshaping the row. The candidates:
  keeping `interface` carried the Zabbix host-interface precedent but a growing collision with real
  network interfaces as monitored objects; `service` read naturally (the SSH service on a host) but
  `public.service` already names the service-principal identity, and one word naming two unrelated
  entities in one schema is the worse collision; `api` read well in operator speech but fights the
  platform's own API-first vocabulary (`internal/api`, `/api/v1`, the generated OpenAPI) in every
  sentence needing both senses. `endpoint` is the word the architecture of record already reached
  for when describing this entity's address, is observability-native (a scrape endpoint, a
  monitoring endpoint) and network-native (the SSH endpoint on a host), and composes cleanly
  (`endpoint:*`, `/endpoints`, `endpoint-reachable`).
