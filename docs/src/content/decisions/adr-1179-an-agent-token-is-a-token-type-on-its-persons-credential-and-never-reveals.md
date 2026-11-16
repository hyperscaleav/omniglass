---
title: "An agent token is a token type on its person's credential and never reveals"
date: 2026-11-16
status: "Accepted"
summary: "An agent token is a credential minted by a person, named for its agent, required to expire, optionally narrowed to roles and channels. Its authority is its roles intersected with its person's current grants, checked on every request; reveal and use are stripped by construction for it and for any token on the MCP channel; it cannot mint, change the password or impersonate; audit_log.agent names it"
refs: [1179, 1162, 1175, 1166]
---

- **Date:** 2026-11-16 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/#agent-tokens), [audit](/architecture/audit/), [agents](/guides/agents/)
- **Decision:** (#1179.) An agent token is a `credential` row on the minting person, marked with an
  agent name, its own roles and a channel allowlist, minted through `POST /auth/me/tokens`; it is not
  a principal (the `agent` kind stays reserved, ADR-0001). `ttl_days` is required for it.
  1. **Intersection, every request.** `authn` resolves the person's roles, then intersects them with
     the token's roles (`rbac.Set.Within`), so a change to the person's grants reaches the agent at
     its next call. A role the minter does not cover is refused at mint. `scopeFor` returns an empty
     scope for an action the constrained set does not allow, so the person's scope never carries the
     agent past its capability.
  2. **The ceiling is by construction.** `rbac.Set.Without("reveal", "use")` drops those actions
     whatever pattern grants them, the owner's `>` included. It applies to an agent token on every
     channel and to **any** token whose request arrives on the `mcp` audit channel, which the MCP
     dispatch sets in-process and no caller can claim or shed. An agent can never decrypt a
     credential for its person; it works through routes that use a credential server-side. Since a
     write that binds a secret to a target makes the node send the plaintext wherever the target
     points, every such write (an endpoint attach or input edit, a plan, a simulated system or
     fleet device) resolves its references under `secretBind`, which is the empty scope under the
     ceiling: until `secret:use` splits from `reveal`, a ceilinged caller binds and repoints no
     credential, and an unconstrained one binds what it may read, as before.
  3. **Refused routes.** An agent-token caller gets a 403 from `authn`, before its body is read, on
     `create-auth-me-token`, `change-auth-me-password` and `impersonate-principal`, on every channel.
     This **amends** the ticket's "impersonation semantics unchanged": an impersonation token is not
     an agent token, so minting one would shed the ceiling and the `audit_log.agent` attribution.
  4. **Channels.** An allowlisted token on another channel is refused (403), except
     `GET /auth/me`, so a client can still learn who it is and why it is refused.
  5. **Escalation guards measure the bounded set.** Granting a role (to a principal or a group),
     adding a group member, resetting a password and impersonating each check that the caller covers
     what it hands over. Under an agent token or the ceiling that check uses the constrained set, not
     the person's grants, so an agent cannot give a principal it controls a capability it is itself
     denied. Adding a group member gains that check only for a constrained caller.
  6. **Audit.** `authn` puts the agent name on the request context; the Storage Gateway's audit
     writer stores it in `audit_log.agent` beside the person as actor. The MCP dispatch needed no
     change: it relays the caller's bearer to the route, where `authn` marks it like any other call.
- **Context:** A person's token in an agent's hands looked exactly like the person on REST and the
  CLI, and carried the person's whole authority, reveal included. The architect's rule is that a
  device credential is always blind to the agent: it may know a credential is present, never its
  value.
- **Consequences:**
  - The `/auth/me` permission hint is conservative under the ceiling: `rbac.Set.Strings` drops any
    pattern that would cover a denied action, so a ceilinged owner lists no `>`. The route check is
    exact; the hint understates.
  - Until `use` is split from `reveal` (its own ticket), routes gated on `secret:reveal` scope that
    only use a secret server-side (a driver preview with secret inputs, a notification target's secret
    binding) are refused to agents and to MCP callers.
  - The hand-written `omniglass token` (direct database, the trusted lane) is not extended: it would
    skip the role-cover check the API makes. The generated `omniglass auth create-token` carries the
    agent flags.
  - Not built here, each tracked on #1179: narrowing an agent token to a location, OAuth 2.1
    authorization for MCP clients (minting this same token), omnibot's marking (its channel is #1166's),
    and the console's agent fields.
- **Not chosen:**
  - **A lightweight service account per agent.** A second identity system (principals, grants,
    lifecycle) for data that does not exist; the credential row already carries purpose, expiry and
    audit.
  - **Ceiling only for agent tokens.** A personal token pasted into an MCP client is an agent's hand
    just the same; keying the ceiling on the channel too closes that without asking the person to
    mint correctly.
  - **Refuse impersonation by handler check.** The refusal lives with the other agent refusals in
    `authn`, keyed by operation id, so one list states what an agent may never call.
