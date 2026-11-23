---
title: "The docs and the vocabulary say health, not verdict (#2104)"
date: 2026-11-22
refs: [2104, 2086]
---

The last of the three renames under ADR-2086. The docs prose says **health** for a system's,
location's or component's result, **reachability** for an endpoint's up or down, and
**settlement** for a command's. The vocabulary table's `verdict:` key is `health:` (its accessor
is `Healths()`, the guard `TestEveryHealthIsATerm`), and a new **health** term carries `verdict`
as a retired word, so the denylist bans it in docs prose, seeded operator strings and generated
API descriptions. The kept senses (the soak, perf and footprint gates, a subscription's test, a
target's test send, the ship-review's `Verdict:` line and a soak record's `verdict:` field) are in
its Allow pattern. Generated descriptions (OpenAPI, CLI and MCP references) were rewritten at their
Go `doc:` tags and regenerated. Decision entries and the build log stay as written.
