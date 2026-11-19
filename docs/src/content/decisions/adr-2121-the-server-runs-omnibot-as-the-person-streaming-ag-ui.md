---
title: "The server runs omnibot as the person, streaming AG-UI"
date: 2026-11-17
status: "Accepted"
summary: "POST /assistant/conversations/{name}:run runs omnibot on the server over the person's stored conversation and streams the run as AG-UI events. The run holds an omnibot token of its own, revoked when it ends, and calls its tools in process through the API router; an ask ends the run on an interrupt the next run's resume answers. Only the person, as themselves, runs it"
refs: [2121, 2113, 2120, 1166, 1167, 2126]
---

- **Date:** 2026-11-17 | **Status:** Accepted | **Pages:** [AI](/architecture/ai/), [API](/architecture/api/), [connect an agent](/guides/agents/#running-omnibot-on-the-server)
- **Decision:** (#2121, on the loop's pause, #2120, and the conversations of [ADR-2113](/architecture/decisions/#adr-2113-a-persons-omnibot-conversations-are-theirs-alone).)
  1. **The wire is AG-UI.** `{name}:run` takes an AG-UI run input and answers `text/event-stream`,
     the run narrated as AG-UI events. The conversation is the thread, and the stored transcript is
     the conversation: of the messages a client sends, only the last user message is read, as the
     prompt. A conformance test holds the server's events to the protocol's published schemas.
  2. **The run is the person's agent.** It mints an agent token for the person, exactly as the CLI
     does (the `omnibot` channel alone, for the run's 30 minutes at most), revokes it when the stream ends, and calls its tools
     in process through the same router and middleware a remote MCP call takes. Grants, scope, the
     agent policy and the audit all read that token, so a write is audited as `omnibot` acting for the
     person, and the model never holds a credential.
  3. **An ask is an interrupt.** A call the policy asks about ends the run with an interrupt naming
     the call; the run's messages and the waiting call are kept with the transcript
     ([ADR-2113.2](/architecture/decisions/#adr-21132-a-paused-omnibot-run-keeps-only-its-transcript)).
     A later run whose `resume` names that interrupt answers it: approved runs the call, anything else
     declines it. A resume naming no waiting call, or a prompt while one waits, is a 409.
     Before any tool runs, the run claims the conversation; a second run, two resumes of one waiting
     call included, is a 409 until the first keeps its transcript, so a call runs once. A run that
     dies frees its claim when its 30 minutes lapse.
  4. **Only the person, as themselves.** An agent token or an impersonation session is a 403, so a
     run's actor is always the conversation's owner. This narrows ADR-2113's impersonation exception
     for runs: impersonation still reads the target's conversations, but does not act as their agent.
  5. **A model is configuration.** A server with no model answers 503. Until a live provider lands,
     `OMNIGLASS_OMNIBOT_REPLAY` names a recording the server plays; tests inject a model in process.
- **Context:** The console panel (#1167) and an event rule's omnibot step (#2126) both need omnibot
  to run somewhere other than a terminal. The 2026-11-16 ruling keeps approvals client-side; an
  interrupt hands the decision back to the client rather than holding it on the server.
- **Not chosen:**
  - **A bespoke event stream.** AG-UI already names text, tool calls, results and interrupts, and
    an off-the-shelf client reads it; a house format would be one more contract to keep.
  - **The run reusing the caller's session.** A session is not an agent: the policy, the channel
    and the audit's agent column would read as the person acting directly.
  - **Believing the client's history.** A client could then feed the model a transcript the
    server never kept, and resume a call that never waited.
