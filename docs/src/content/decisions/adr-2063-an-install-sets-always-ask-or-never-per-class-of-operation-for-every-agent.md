---
title: "An install sets always, ask or never per class of operation for every agent"
date: 2026-11-17
status: "Accepted"
summary: "Every operation has one agent class (read, write, destructive, credential destination). An admin sets always, ask or never per class in the agent settings namespace; the server refuses a never class to any agent request and audits it as the agent's, and the MCP tool list carries each tool's class and policy in its _meta for the client to ask on"
refs: [2063, 1161, 2088, 1166]
---

- **Date:** 2026-11-17 | **Status:** Accepted | **Pages:** [AI](/architecture/ai/), [connect an agent](/guides/agents/#what-an-agent-may-do), [settings](/architecture/settings/)
- **Decision:** (#2063, slice 1.)
  1. **One class per operation, the most sensitive wins.** An operation gated on `secret:bind`,
     directly or as its conditional second gate, is `credential_destination`; else a destructive
     one (PATCH, PUT, DELETE, or stamped so) is `destructive`; else a read-only one is `read`; else
     `write`. The class is derived from the same annotation rule as the MCP hints, once in
     `tooldef.Classify`, and a test holds the generated catalog's class equal to the server's for
     every tool.
  2. **A platform namespace, defaults refusing nothing.** `agent` holds one of `always`, `ask`,
     `never` per class; reads default to `always` and the rest to `ask`, so an install that sets
     nothing lets an agent do what its person may, as the agent token ruling holds.
  3. **The server enforces only never.** Right after authentication, a request under the agent
     ceiling (an agent token, or any call over MCP) whose class is `never` is a 403 with an
     `agent_refused` audit row naming the operation, written as the agent's. The policy is re-read on
     each such request, so a change takes effect on the next call. `ask` is the client's: there is no
     server-side pending approval, the person approves in their own client.
  4. **Two operations are exempt.** Reading who you are and reading the agent policy are never
     refused, so a refused agent can learn why.
  5. **No agent rewrites the policy.** Patching or dropping the `agent` namespace, and the factory
     reset that drops it with the rest, are refused to every agent request whatever the classes say,
     audited as `agent_refused`. Otherwise an admin's agent with `write` set to `always` could set
     its own `destructive` back to `always`, and the policy would bind only the agents that leave it
     alone.
  6. **The tool list carries the policy.** Each tool in `tools/list` carries
     `_meta["omniglass/agent_policy"] = {class, policy}`, read with the caller's own token. A
     `never` tool is still listed, marked, so the client may hide it. If the policy cannot be read the
     list goes out unmarked, since the server enforces it anyway.
  7. **The caller reads its own policy.** `GET /auth/me/agent-policy` (`omniglass auth agent-policy`)
     answers each class's policy and whether it governs this request; the namespace itself stays
     admin-read.
- **Context:** Agents act with their person's grants, approvals happen client-side, and the server
  must still let an install rule out a whole class for every agent at once.
- **Limits:** A per-role or per-tool override is #2088; the in-app assistant's ask is #1166. The class
  is static per operation, so an update that binds a secret only sometimes is always
  `credential_destination`.
- **Not chosen:**
  - **Server-side pending approvals.** They would queue writes for a person who is already at the
    client the agent runs in.
  - **Hiding never tools from the list.** The list would then depend on a setting the client cannot
    see, and the refusal is the server's either way.
  - **A class read from the request body.** It would make the class depend on input; a static class
    is one the reference can print.
