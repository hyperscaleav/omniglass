---
title: "Binding a secret takes secret:bind, and an agent may bind (#2047)"
date: 2026-11-16
refs: [2047, 1179, 2063]
---

Handing a secret to a target (an endpoint input, a driver preview, a template plan, a simulated
device, a notification target, the mail relay) now takes `secret:bind` on the secret, not
`secret:reveal`. A secret the caller cannot read keeps its non-disclosing refusal; one it can read
but not bind is a 403 naming `secret:bind`. The agent ceiling shrinks to the plaintext-out actions
(`reveal`, `enroll`), so an agent token binds what its person may bind, audited as the agent's,
with approval left to the MCP client ([ADR-2047](/architecture/decisions/#adr-2047-an-agent-may-do-what-its-person-may-except-take-a-credential-out-in-plaintext)).
`operator` and `deploy` gain `secret:bind`. Before, `TestAgentTokenBindsACredentialItNeverReads`,
`TestAnAgentRunsAPollWithACredentialItNeverReads`, `TestHandingASecretToANodeTakesSecretBind` and `TestBindingTheRelaysSecretTakesSecretBind` failed (an
agent's attach was refused, and a role with `secret:bind` but no reveal could not bind); they pass
now, beside the notification target and preview cases.
