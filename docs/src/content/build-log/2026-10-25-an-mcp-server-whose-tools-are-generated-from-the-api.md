---
title: "an MCP server whose tools are generated from the API (#1162)"
date: 2026-10-25
refs: [1162, 1161]
---

The first slice of epic #1161: omniglass serves MCP, so an operator's own agent (Claude Code, or any
MCP client) drives the fleet through the same API every other client does
([ADR-1162](/architecture/decisions/#adr-1162-the-mcp-server-is-a-generated-client-of-the-operations-stamped-for-it-run-as-the-callers-token)).
An operation is exposed by a stamp at its registration in Go (`Extensions: mcpExposed()`), and
`cmd/mcpgen`, beside `cligen` in `make gen`, generates the tool catalog (`internal/mcp/catalog_gen.go`)
and the [MCP tools reference](/reference/mcp/) from `api/openapi.json`. The first catalog is 46 tools:
the reads of places, systems, components, nodes, the catalog, endpoints, secrets (masked),
reachability, samples, events, logs and a command's status, and the writes an agent setting up monitoring needs (create a
component, an endpoint, a secret or a driver, edit and retry an endpoint, issue a command). One pure
rule (`tooldef.Annotate`) annotates each tool read-only, destructive, idempotent or open-world from its
method and gate, and a stamp can only add caution.

The server is mounted at `/mcp` over streamable HTTP, and `omniglass mcp` relays stdio to it for an
agent that launches a local command; `omniglass mcp tools` and `omniglass mcp call` are a thin stdio
client of that relay. Both transports answer only to a bearer token the API accepts, and every tool
call runs its route through the same router with that token on a fresh request context, so the gate,
the scope and the same-transaction audit row are the route's own. The relay sends the token only to
the server it was given and follows no redirect, and renews its session when the server has let
it go (an idle timeout, a restart), resending only a call the server refused before running it. A
route that panics behind a tool answers that call 500 instead of crashing the server.

`audit_log` gained `channel` and `client`: an MCP call's row reads `channel: mcp` and the client's
declared name, set on the request context by the dispatch so no direct caller can claim one, and
`GET /audit-log` returns both. Minting and revoking a token now write `credential` audit rows, which
they never did, since a token is how an agent is given access: the bootstrap token's mint (with no
actor) and a logout that presents a token as its session included. The CLI reference generator learned to
document a command group that is itself runnable, which `omniglass mcp` is.

Proof: an e2e test builds the binary, runs the server, a simulator and a node, and drives `omniglass
mcp tools` and `omniglass mcp call` over stdio, reading a simulated component's reachability verdict
and writing a secret whose audit row names the user and the MCP channel; API-level tests drive a real
MCP client over HTTP through the gate (a viewer's write refused), the scope (a component-scoped
principal sees its component alone), revocation mid-session and impersonation. The `mcp-server` tape
demos it. The adversarial review's security lens found the auth, the channel guard and the relay's
token handling sound; it caught a 70 MB generator binary about to be committed (now ignored, with
the older stray `cligen` filed as #1174), a panic path, the relay's lost session, two annotations
too weak (retrying an endpoint is destructive; the endpoint writes and issuing a command are
open-world), and two token paths the audit missed, each fixed with its test.
