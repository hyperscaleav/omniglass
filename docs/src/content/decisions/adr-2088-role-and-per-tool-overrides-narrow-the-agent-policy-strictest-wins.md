---
title: "Role and per-tool overrides narrow the agent policy, strictest wins"
date: 2026-11-17
status: "Accepted"
summary: "The agent settings namespace gains roles (a class per role name) and tools (a policy per operation ID). The resolved policy for a request is the strictest of the platform class, every role the person is granted and the tool, a tie naming the earlier layer, and every answer and refusal names the layer that set it"
refs: [2088, 2063, 1161]
---

- **Date:** 2026-11-17 | **Status:** Accepted | **Pages:** [AI](/architecture/ai/), [connect an agent](/guides/agents/#narrowing-it-by-role-or-by-tool), [settings](/architecture/settings/)
- **Decision:** (#2088, extending [ADR-2063](/architecture/decisions/#adr-2063-an-install-sets-always-ask-or-never-per-class-of-operation-for-every-agent).)
  1. **Both overrides live in the agent settings namespace.** `agent.roles` maps a role name to a
     partial set of classes, and `agent.tools` maps an operation ID to `always`, `ask` or `never`.
     They are platform settings like the classes, so they share its audit, its admin-only read and
     its rule that no agent may rewrite them. No migration and no column on the role table.
  2. **Strictest wins, and only narrows.** A request's policy for its operation is the strictest of
     the class's platform value, that class under every role the person is granted, and the tool's
     own value. A role or a tool can make an agent stricter, never looser: a tool set to `always`
     under a `never` class is still refused.
  3. **Granted roles, direct or through a group.** The roles are the person's grants, not a token's
     narrowed set, so an agent token minted with fewer roles cannot step out from under an override
     that binds its person.
  4. **Every value names its layer.** `GET /auth/me/agent-policy` answers each class with its
     `source` (`platform`, `role` with the role, or `tool`) and lists every overridden tool the same
     way; on a tie the earlier layer is named (platform, then roles by name, then the tool). The 403
     says which layer refused.
  5. **Overrides are checked when written.** An unknown role, an unknown operation, or one of the two
     exempt reads is a 422 on the patch, so a typo cannot sit in the policy binding nothing.
- **Context:** #2088 asked for overrides stored with the role. Roles are seed-only with no operator
  write path, so a role column would have needed a write path of its own before an admin could use it.
- **Limits:** A role override names a role, not a group or a user. Inherited roles do not count, only
  granted ones. The in-app assistant's ask is #1166.
- **Not chosen:**
  - **A column on the role table.** Roles have no write path today; the settings namespace already
    has its patch, audit and self-rewrite guard.
  - **Most specific wins** (a tool override beating its class either way). It would let an override
    loosen the install's floor, which is the one thing an admin setting a `never` has to rely on.
