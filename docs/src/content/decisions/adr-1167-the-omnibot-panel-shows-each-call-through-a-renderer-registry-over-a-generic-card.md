---
title: "The omnibot panel shows each call through a renderer registry over a generic card"
date: 2026-11-17
status: "Accepted"
summary: "The console's omnibot panel reads the :run AG-UI stream with a small reducer of its own, renders every tool call as a generic card unless a per-tool renderer reads its result, pauses an ask on a confirm card the next run resumes, and hands omnibot a credential by name, created through the secrets route with the person's token"
refs: [1167, 2121, 2134, 2135, 2111, 1168]
---

- **Date:** 2026-11-17 | **Status:** Accepted | **Pages:** [AI](/architecture/ai/), [connect an agent](/guides/agents/#omnibot-in-the-console), [console](/operator/#getting-around)
- **Decision:** (#1167, over the run of [ADR-2121](/architecture/decisions/#adr-2121-the-server-runs-omnibot-as-the-person-streaming-ag-ui).)
  1. **One panel, two doors.** A drawer opened from the top bar or the command palette's last row,
     shown only to a person who may use the assistant. The palette's query arrives as a draft, sent
     only when the person sends it.
  2. **A generic card first, renderers over it.** Every tool call is a card naming the tool and its
     arguments, with the raw result one click away. A registry keyed by tool name may render the
     result the way the console shows that thing elsewhere (a component as a link, reachability as
     the endpoint ladder); a tool with no renderer, or a result the renderer cannot read, keeps the
     generic card, so a tool added to the catalog is never a blank.
  3. **The run's end is the stream's.** An ask is the RUN_FINISHED interrupt, shown as a confirm
     card whose answer is the next run's `resume`; the step limit is read from the RUN_FINISHED
     result; a 403 reads as refused and any other failure names the server's reason.
  4. **A credential goes by name.** **Add a credential** creates the secret through the existing
     secrets route with the person's own token and puts only `secret <name>` in the draft, the
     console's form of the CLI's `/secret`.
  5. **Deferred, each filed.** A driver spec and first samples renderer is #2134, reopening an
     earlier conversation #2135, the over-budget state #2111, and the confirm card's flag for a run
     that read external content waits on #1168.
- **Context:** The 2026-11-16 ruling keeps approvals in the client and the run as the person's
  agent; the panel is the console's client. The catalog grows by generation, so the panel cannot
  depend on a renderer existing for every tool.
- **Not chosen:**
  - **`@ag-ui/client`.** The panel needs a handful of event types and one HTTP shape; a pure reducer
    over the server's own fixtures is smaller and tests without a network.
  - **A renderer per tool before a generic card.** Every new tool would land blank until someone
    wrote its view.
  - **A server-side approval queue.** Ruled out on 2026-11-16: the decision is the client's, and
    the interrupt hands it back.
