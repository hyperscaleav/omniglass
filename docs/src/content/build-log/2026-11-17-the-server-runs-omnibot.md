---
title: "The server runs omnibot, streaming AG-UI (#2121)"
date: 2026-11-17
refs: [2121, 2113, 2120]
---

`POST /assistant/conversations/{name}:run` runs omnibot on the server over one of the person's stored
conversations and streams the run as AG-UI events (ADR-2121). The run mints its own one-day omnibot
token, calls its tools in process through the API router (so grants, the agent policy and the audit
read it as they read a remote agent), and revokes it when the stream ends. A write the policy asks
about ends the run on an interrupt kept with the transcript; the next run's `resume` approves or
declines it. Agents and impersonation sessions are refused, a server with no model answers 503, and
`OMNIGLASS_OMNIBOT_REPLAY` plays a recording until a live provider lands. The lab's server plays an embedded one, written by its seed. A web conformance test holds
the narrated events to `@ag-ui/core`'s schemas. The console panel that drives it is #1167.
