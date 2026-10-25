---
title: Connect an agent
description: "Point Claude Code, or any MCP client, at omniglass: the MCP server's tools are generated from the API and run with your token."
---

Omniglass serves the **Model Context Protocol** (MCP), so an AI agent you already use (Claude Code, or
any MCP client) can read your fleet and work in it. The agent drives omniglass through the same API as
the console and the CLI: each MCP tool is one API operation, generated from the API by `make gen`, and
each call runs with **your token**, so the agent sees and changes exactly what your permissions and
scope allow and every change it makes is audited as yours, marked as having arrived over MCP. The
[MCP tools reference](/reference/mcp/) lists every tool, its gate, and what a client may assume of it.
The design is on the [API page](/architecture/api/#also-an-mcp-surface).

## Give the agent a token of its own

Mint a token for the agent rather than lending it yours, so you can revoke it alone:

```bash
omniglass auth create-token --description "claude code on my laptop" --ttl-days 30
```

The token is printed once. It carries your whole authority (there is no narrower token yet), so treat it
like your password: keep it in the agent's environment, not in a file you commit. Minting it writes an
audit row, and so does revoking it later: `omniglass session list` shows it, and
`omniglass session revoke <id>` ends it.

## Over HTTP

A remote agent connects to `/mcp` on your server over streamable HTTP, sending the token as a bearer
header. The endpoint answers to a token and nothing else (a browser's signed-in session does not open
it). With Claude Code:

```bash
claude mcp add --transport http omniglass https://omniglass.example.com/mcp \
  --header "Authorization: Bearer $OMNIGLASS_TOKEN"
```

Any MCP client that takes a JSON server list takes the same three facts:

```json
{
  "mcpServers": {
    "omniglass": {
      "type": "http",
      "url": "https://omniglass.example.com/mcp",
      "headers": { "Authorization": "Bearer ${OMNIGLASS_TOKEN}" }
    }
  }
}
```

Use https for anything beyond your own machine: the token travels in every request.

## Over stdio

An agent that launches its MCP servers as local commands runs `omniglass mcp`, which serves the same
tools over stdio by relaying to the server's `/mcp` with the token in its environment:

```bash
claude mcp add omniglass \
  -e OMNIGLASS_SERVER=https://omniglass.example.com -e OMNIGLASS_TOKEN="$OMNIGLASS_TOKEN" \
  -- omniglass mcp
```

The relay serves exactly the tools the server lists, sends the token only to the server you named, and
never follows a redirect elsewhere.

## Try it from a terminal

`omniglass mcp tools` and `omniglass mcp call` are a small MCP client over stdio, the same path an agent
takes, which is handy for checking a connection or scripting a tool:

```bash
omniglass mcp tools | jq -r '.[] | "\(.name)\tread-only=\(.read_only)"'
omniglass mcp call get-component-reachability --args '{"name":"panel-1"}'
```

A tool the route refuses (no permission, a component outside your scope, a bad argument) prints the
route's own error and exits non-zero.

## What the agent may assume, and what it should ask

Every tool carries the MCP annotations, from one rule over its route's method and gate: **read-only**
tools change nothing, **destructive** ones overwrite, remove, or act on a component (issuing a command
is one), and the rest add something. A well-behaved client runs the read-only tools freely and holds
anything else for your confirmation; most clients do this by default, and you can tighten it in the
client's own permission settings.

## Seeing what it did

An agent's writes are yours in the [audit trail](/guides/admin/audit/), with how they arrived beside
them. An admin reads it with:

```bash
omniglass audit-log list --resource component --limit 5 | jq '.events[] | {verb, actor_name, channel, client}'
```

A row with `"channel": "mcp"` came from an MCP tool call, and `client` names the client the agent
declared itself as (asserted by the client, not verified).
