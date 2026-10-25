---
title: "The MCP server is a generated client of the operations stamped for it, run as the caller's token"
date: 2026-10-25
status: "Accepted"
summary: "An API operation joins the MCP catalog by an x-omniglass-mcp stamp at its registration in Go, and cmd/mcpgen, beside cligen, generates the tools and their docs reference from api/openapi.json: generated, not hand-listed, and curated, not one tool per operation. Annotations come from one pure rule over the method and the gate; a stamp can only add caution. The server is served at /mcp over streamable HTTP and over stdio by omniglass mcp, a relay to it; both admit only a bearer token the API accepts, and every tool runs its route through the router with that token, so the gate, the scope and the audit row are the route's. The row records how it arrived in new audit_log.channel and client columns, set server-side. Minting and revoking a token are audited"
refs: [1162, 1161]
---

- **Date:** 2026-10-25 | **Status:** Accepted | **Pages:** [api](/architecture/api/),
  [AI](/architecture/ai/), [audit](/architecture/audit/),
  [identity and access](/architecture/identity-access/), [API first](/contributing/api-first/),
  [connect an agent](/guides/agents/), [MCP tools](/reference/mcp/)
- **Decision:** (#1162, the first slice of epic #1161, whose decisions 1 and 2 the architect ruled
  as recommended.)
  1. **Generated, and curated in Go.** The API page and the AI page said the catalog is curated, not
     one tool per operation; #1161 asked for tools generated like the CLI. Both hold: an operation
     is exposed by a stamp at its registration (`Extensions: mcpExposed()`), which puts the
     `x-omniglass-mcp` extension in the generated OpenAPI beside `x-omniglass-permission`, and
     `cmd/mcpgen` turns every stamped operation into a tool. What is exposed is reviewed in the same
     diff as the route, never in a hand list that drifts from it. The first catalog is the reads
     and the core writes an agent setting up monitoring needs (46 tools of 298 operations): a model
     chooses worse among hundreds. A tool is named by its operation id, describes itself with the
     operation's description (which names its gate), and takes the path parameters, query
     parameters and body properties as one argument object, references inlined; a name used
     twice, a required header, or a stamp the rule refuses fails the generator. A drift test
     rebuilds the catalog from the committed spec in `make test`, and `gen-drift` diffs it in CI.
  2. **Annotations are one rule, and a stamp only adds caution.** A tool is read-only when its
     route is a GET or a POST gated by a read (a preview, a validate); a PATCH, PUT or DELETE is
     destructive; a POST is additive unless its stamp says destructive (issuing a command does, and
     so does retrying an endpoint, which can replace the key it trusts); reads, PUTs and DELETEs
     are idempotent; open-world comes only from a stamp, set where a node reaches the network on
     the call's account (creating, editing or retrying an endpoint, issuing a command). Every hint
     is sent explicitly, since the protocol's defaults are the cautious opposite. A stamp has no
     read-only hint, so it cannot present a write as a read; a destructive hint on a read is
     refused as a contradiction. A route that panics behind a tool is that call's 500, recovered
     in the dispatch, since a tool runs off the HTTP goroutine where net/http's recovery does not
     reach.
  3. **One implementation, behind the caller's token.** The server is mounted at `/mcp` beside the
     API, not inside it, and dispatches each tool through the same router with the caller's
     Authorization header on a fresh request context: nothing ambient from the MCP request rides
     in, and a session cookie is never forwarded. The endpoint admits a request only when the API
     accepts its bearer token (it asks `GET /auth/me`), so expiry, revocation and impersonation
     behave as on every route, and a browser's cookie cannot drive it. Stdio is `omniglass mcp`, a
     relay that connects to `/mcp` with `OMNIGLASS_TOKEN`, serves exactly the tools the server
     lists and forwards each call with the calling client's declared identity; the token goes only
     to the named origin and never across a redirect. A call the server refuses for an expired
     session (its idle timeout, or a restart) is sent once more over a fresh one, since it was
     never dispatched; a dropped connection renews the session for the next call without
     retrying, so a write never runs twice. The token is the authority: there is no
     `mcp:*` permission, and no OAuth for MCP until the OIDC work (ADR-0004).
  4. **The audit row says how it arrived.** `audit_log` gains `channel` (`mcp`, with `omnibot`
     reserved for #1166) and `client` (the name and version the MCP client declared, sanitized and
     recorded as asserted). The dispatch sets them on the request context, which only server code
     can reach, so a direct API caller cannot claim a channel; a check holds the column to the
     channels the server knows. The actor stays the principal (and the real actor under
     impersonation): the channel is the AI-sourced marking the audit page promised, riding beside
     the row, not a new actor.
  5. **A token is audited.** A token is how an agent is given access, so minting one writes a
     `create` row (the bootstrap token too, with no actor, from the trusted lane) and revoking one
     (one at a time, in bulk, or by logging out with it as the session) a `revoke` row, on resource
     `credential`, keyed by the credential's id and carrying its prefix, purpose, description and
     expiry, never the token or its hash. A web-login session stays the auth lane's.
- **Context:** The design pages fenced the MCP surface under #522, and `api-first.md` listed an MCP
  catalog as a stage with no generator. #1161 needed one so that omnibot and an operator's own agent
  drive omniglass through the same surface; making it a generated client of the route registry is
  what makes "not a side channel" true by construction rather than by review.
