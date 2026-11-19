---
title: "An install sets what an agent may do (#2063)"
date: 2026-11-17
refs: [2063, 1161, 2088]
---

An admin had no way to rule out a kind of action for every agent at once: an agent could do whatever
its person could, and only the client decided what to ask about. The `agent` settings namespace now
sets `always`, `ask` or `never` for each class of operation (read, write, destructive, credential
destination), with defaults that refuse nothing. The server refuses a `never` class to any agent
token or MCP call with a 403 and an `agent_refused` audit row as the agent's, every MCP tool lists its
class and the caller's policy in its `_meta`, and `omniglass auth agent-policy` reads the policy back.
Before, `TestAgentPolicyNeverRefusesTheClassOnEveryChannel` and `TestAgentPolicyMarksEveryListedTool`
failed (the route and the setting did not exist); they pass now, with
`TestEveryToolsClassIsTheServersClass` holding the catalog's class to the server's. Per-role and
per-tool policy is #2088.
