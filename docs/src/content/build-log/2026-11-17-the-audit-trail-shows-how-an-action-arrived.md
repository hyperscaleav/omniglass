---
title: "The audit trail shows how an action arrived (#1178)"
date: 2026-11-17
refs: [1178, 1162, 1179]
---

The trail recorded an MCP call's channel and client and an agent token's agent, but only the raw
API returned them, and nothing could filter on the first two. `GET /audit-log` now takes `channel`
(`direct` for a plain API call, `mcp`, `omnibot`; anything else is a 422) and `client` (a
case-insensitive contains), and the generated CLI and MCP tool carry both. The console's Admin >
Audit page gains a Channel column (an `mcp` pill over the declared client, nothing on a direct row),
an `agent <name>` tag beside the person in Who, `channel`, `client` and `agent` facets, and drawer
entries whose info tip teaches what a channel proves and what it does not. Before,
`TestAuditLogFiltersByChannelAndClient` failed (the parameters were unknown and every row returned)
and the seven console tests found no channel; they pass now.
