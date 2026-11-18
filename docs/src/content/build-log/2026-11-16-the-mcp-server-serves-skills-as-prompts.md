---
title: "The MCP server serves skills as prompts (#2026)"
date: 2026-11-16
refs: [2026, 1162, 2025]
---

An agent connected to omniglass got tools and no idea of the order a job takes. Skills are now one
`SKILL.md` each, embedded in the binary and served as MCP prompts, starting with `triage-alarm` and
`troubleshoot-component`; the stdio relay mirrors them, `omniglass mcp skill` prints them, and
`make gen` renders the [MCP skills reference](/reference/mcp/skills/) from the same files. Before,
`TestTheServerServesSkillsAsPrompts` and `TestRelayServesTheServersPrompts` failed (the server listed
no prompts) and `TestEverySkillToolIsInTheCatalog` did not build; they pass now, with the loader's
refusal cases and the `omniglass mcp skill` test beside them. Acknowledging and snoozing an alarm wait
for their tools (#2025), so the triage skill proposes them for the console until then.
