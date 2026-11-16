---
title: "Agent tokens act on a person's behalf and never reveal (#1179)"
date: 2026-11-16
refs: [1179]
---

A person can now mint an agent token: `POST /auth/me/tokens` with an `agent` name, a required
`ttl_days`, and optional `roles` and `channels`. Its authority is its roles intersected with the
person's current grants on every request, a role wider than the minter is refused at mint, and a
channel allowlist refuses the other channel. Reveal and use are stripped by construction for an agent
token and for any token used through MCP, so an agent cannot decrypt a credential; it also cannot
mint a token, change the password or impersonate. Nor can it aim one: a write that binds a secret to
a target (an endpoint attach or input edit, a plan, a simulated system or fleet device) resolves its
references under an empty secret scope under the ceiling, so an agent cannot repoint a
credential-bearing endpoint at a host it controls (`TestAgentTokenCannotSendACredentialToATarget`
failed first, the agent's attach landing with the secret bound). Every write it makes carries `audit_log.agent`
beside the person as actor, filterable with `GET /audit-log?agent=` and shown in the session list
([ADR-1179](/architecture/decisions/#adr-1179-an-agent-token-is-a-token-type-on-its-persons-credential-and-never-reveals)).
`TestAgentTokenCannotReveal` failed before the ceiling (every reveal route returned plaintext) and
passes after, alongside the intersection, refusal and channel e2e tests and the `rbac` `Within` and
`Without` unit tests. OAuth 2.1 for MCP clients, location narrowing, omnibot's marking and the console
fields follow.
