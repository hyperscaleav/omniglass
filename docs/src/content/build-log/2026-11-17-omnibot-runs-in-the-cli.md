---
title: "omnibot runs in the CLI, asking before each write (#1166)"
date: 2026-11-17
refs: [1166, 1161]
---

`omniglass omnibot <prompt>` runs Omniglass's own agent against the MCP tools. It mints a one-day
agent token held to the new `omnibot` channel from the person's token, so it holds what they hold,
and revokes it when the run ends; its rows audit as `channel = 'omnibot'` and `agent = omnibot`. A
tool's agent policy decides on the client: `always` runs, `ask` is put to the person on the terminal
(with no terminal, declined), `never` is not offered. The model is a recorded conversation
(`--replay`) until a live provider lands. Before, `TestOmnibotRunsConfirmedWritesAsItself` and
`TestOmnibotHoldsOnlyItsPersonsGrants` had no client to drive and the `omnibot` channel could not be
stamped; they pass now, with the token revoked at the end and a viewer's write refused by the route.
