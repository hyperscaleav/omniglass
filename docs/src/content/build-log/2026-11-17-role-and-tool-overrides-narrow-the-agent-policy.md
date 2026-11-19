---
title: "Role and tool overrides narrow the agent policy (#2088)"
date: 2026-11-17
refs: [2088, 2063]
---

The agent policy was one value per class for the whole install, so an admin could not keep
operators' agents from writing without stopping every agent, or refuse one dangerous operation
without refusing its class. The `agent` settings now take `roles` (a class per role name) and
`tools` (a policy per operation), each only narrowing: the strictest of the class, the person's
granted roles and the tool wins. `omniglass auth agent-policy` names the layer behind each value and
lists every overridden tool, and a refusal says whether a tool, a role or the class refused it.
Before, `TestAgentRoleOverrideNarrowsOnlyItsHolders` and `TestAgentToolOverrideRefusesOneTool`
failed (the settings were unknown keys); they pass now. The console edits the two maps as JSON.
