---
title: "A person's omnibot conversations are theirs alone"
date: 2026-11-17
status: "Accepted"
summary: "Stored omnibot conversations are read and written by their owner only, fenced by an owner predicate on every query rather than by a scope set, so no grant (the platform owner's included) reaches another person's. The one exception is impersonation, the support path that sees what its target sees. Appends name the last message seen, and a conversation is pruned by when it last moved"
refs: [2113, 1166, 2120]
---

- **Date:** 2026-11-17 | **Status:** Accepted | **Pages:** [AI](/architecture/ai/), [connect an agent](/guides/agents/#carrying-a-conversation-on), [identity and access](/architecture/identity-access/), [storage](/architecture/storage/#retention-what-is-kept-and-for-how-long)
- **Decision:** (#2113, building on [ADR-1166](/architecture/decisions/#adr-1166-omnibot-is-an-mcp-client-on-its-own-agent-token-confirming-in-its-client). What a paused run keeps is [ADR-2113.2](/architecture/decisions/#adr-21132-a-paused-omnibot-run-keeps-only-its-transcript).)
  1. **Owner-only, by predicate.** A conversation belongs to the user that created it. Every
     gateway read and write takes the caller as its owner predicate, so anyone else's conversation is
     a non-disclosing 404 on every verb and absent from their list, an admin's and the platform
     owner's `>` included. The permission, `assistant:use`, rides `viewer`; it says who may keep
     conversations at all, never whose they may read. The scope resolver records the resource as
     fleet-wide on purpose: it is not placed on any tier.
  2. **Impersonation is the one exception.** A view-as or act-as session is its target for every
     read, so it sees the target's conversations as it sees the rest of what the target sees, and an
     act-as write is audited with the real actor. This is the documented support path, not a grant:
     impersonation is already bounded ([identity and access](/architecture/identity-access/)), so only
     a holder whose own capabilities cover the target may start one, an owner account cannot be
     impersonated, and the session itself is audited.
  3. **Appends name the last message seen.** An append carries `after`, the seq of the client's last
     message; anything else is a 409, so two clients cannot interleave one conversation. Each write is
     audited without its content.
  4. **Retention by last change.** A conversation is pruned `assistant.conversation_days` (30 by
     default) after it last moved, its messages with it, in the retention pass's bounded chunks.
- **Context:** A conversation outlives the session that started it once it is kept, so who reads it
  has to be settled before anything stores one. #2113's first cut fenced it from every role; the
  2026-11-17 ruling kept impersonation's reach, since excluding one resource from it would make the
  support path see less than the person it supports.
- **Limits:** Usage is totalled only when a client reports it, and the recorded provider reports none
  (the relay's per-turn usage is #2111). The console panel is #1167.
- **Not chosen:**
  - **Scope-fenced conversations** (an admin reads anyone's by grant). A transcript can hold what a
    person pasted; it is theirs like a draft, and the audit trail, not the transcript, is what an
    admin reads. Impersonation differs from a grant: it is a bounded, audited session that sees one
    person's view, not a standing reach into everyone's.
  - **Fencing conversations from impersonation too.** It would make the one resource a support
    session cannot see the one the person is asking about.
