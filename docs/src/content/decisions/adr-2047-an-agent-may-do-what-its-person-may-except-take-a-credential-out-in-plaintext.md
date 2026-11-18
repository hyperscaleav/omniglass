---
title: "An agent may do what its person may, except take a credential out in plaintext"
date: 2026-11-16
status: "Accepted"
summary: "Amends ADR-1179. Handing a secret to a target is its own action, secret:bind, separate from secret:read (seeing it exists) and secret:reveal (its plaintext). Every write that binds a secret takes it. The agent ceiling strips only the actions whose output is a credential in plaintext (reveal, enroll), so an agent may bind what its person may bind; approval of an agent's tool calls is the MCP client's (always, ask, never), not a server-side hold, and every write is audited as the agent's"
refs: [2047, 1179, 2063, 2048]
---

- **Date:** 2026-11-16 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/#agent-tokens), [alarms and actions](/architecture/alarms-actions/), [connect an agent](/guides/agents/), [secrets](/guides/admin/secrets/)
- **Decision:** (#2047, ruled 2026-11-16, amending
  [ADR-1179](#adr-1179-an-agent-token-is-a-token-type-on-its-persons-credential-and-never-reveals).)
  1. **`secret:bind` is the action that hands a secret to a target.** An endpoint attach, an edit of
     a secret-bound endpoint's inputs or node, a plan from a template, a simulated system or fleet
     device, a driver preview, a notification target and the mail relay each resolve a secret
     reference through `secretBind`: the reference must be within `secret:read` scope (else the
     non-disclosing refusal, as before) and within `secret:bind` scope (else a 403 naming
     `secret:bind`), with the admin tier for an admin-sensitive secret. Binding no longer rides on
     `secret:reveal`: a role can bind a credential it can never read, which is the role an agent
     needs.
  2. **The seed.** `operator` and `deploy` hold `secret:bind` beside `reveal`; `admin` and `owner`
     reach it through `secret:>`. A custom role that binds secrets names `secret:bind`.
  3. **The ceiling is plaintext-out only.** `agentCeilingActions` is `reveal` and `enroll`: reveal and
     copy return a secret's plaintext, and an enrollment token is itself a node credential (a minted
     API token is already refused to an agent token by route). Every other action the person holds,
     `secret:bind` included, the agent holds, on every channel.
  4. **Approval is client-side.** Binding chooses where a credential goes, so an agent's bind can
     route a credential to a host it picked. That is accepted: the person approves an agent's tool
     calls in their MCP client (always, ask, never per tool, like any MCP client), every write is
     audited with `audit_log.agent`, and platform or role defaults for that policy, with "never"
     enforced on the agent channels, are #2063.
- **Context:** ADR-1179 made binding wait on `secret:reveal` and so left a ceilinged agent unable to
  attach any credentialed endpoint, which kept agents from routine onboarding their person does
  every day. The ruling: if a person has permission to do something, their agent should be able to
  do it for them; the one hard line is that a credential's plaintext never reaches the agent.
- **Limits:**
  - A bound credential still leaves the platform toward the target named; `secret:bind` decides who
    may choose that target, not whether the target is trustworthy (the egress contract still judges
    where the platform itself dials).
  - The approval policy defaults are not built yet (#2063); until then an MCP client's own setting
    is the only approval.
- **Not chosen:**
  - **Server-side pending approvals** for an agent's write. It makes every agent action a two-party
    flow and duplicates what MCP clients already do per tool.
  - **Keeping binding behind `reveal`.** It ties the right to aim a credential to the right to read
    it, so the role that should bind blind (an agent, an onboarding role) cannot.
  - **Calling the action `use`.** Every route that polls with a credential uses it; binding is the
    specific act of choosing where it goes.
