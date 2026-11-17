---
title: "A person's omnibot conversations are theirs alone, and a paused run keeps only its transcript"
date: 2026-11-17
status: "Accepted"
summary: "Stored omnibot conversations are read and written by their owner only, fenced by an owner predicate on every query rather than by a scope set, so no grant (the platform owner's included) reaches another person's. A run that paused on an ask keeps the call it waits on as a marker in its transcript; nothing on the server acts on it, and the owner decides it in their own client"
refs: [2113, 1166, 2063, 2120]
---

- **Date:** 2026-11-17 | **Status:** Accepted | **Pages:** [AI](/architecture/ai/), [connect an agent](/guides/agents/#carrying-a-conversation-on), [identity and access](/architecture/identity-access/), [storage](/architecture/storage/#retention-what-is-kept-and-for-how-long)
- **Decision:** (#2113, building on [ADR-1166](/architecture/decisions/#adr-1166-omnibot-is-an-mcp-client-on-its-own-agent-token-confirming-in-its-client) and the loop's pause, #2120.)
  1. **Owner-only, by predicate.** A conversation belongs to the principal that created it. Every
     gateway read and write takes the caller as its owner predicate, so anyone else's conversation is
     a non-disclosing 404 on every verb and absent from their list, an admin's and the platform
     owner's `>` included. The permission, `assistant:use`, rides `viewer`; it says who may keep
     conversations at all, never whose they may read. The scope resolver records the resource as
     fleet-wide on purpose: it is not placed on any tier.
  2. **A kept transcript is not a pending approval.** A run that paused on an `ask` stores the
     conversation with a marker naming the call it waits on. The server never runs, approves,
     expires or notifies on that marker: it is the transcript's last word, kept for its owner. The
     owner continues the conversation in their own client, which rebuilds the proposal from the
     transcript's open call and the tools as they are now (a marker naming no open call is refused)
     and decides it as an inline ask is decided. The approval stays client-side
     ([ADR-2063](/architecture/decisions/#adr-2063-an-install-sets-always-ask-or-never-per-class-of-operation-for-every-agent)),
     and the eventual call is the owner's agent's like any other, re-checked against their grants and
     the policy when it is sent.
  3. **Appends name the last message seen.** An append carries `after`, the seq of the client's last
     message; anything else is a 409, so two clients cannot interleave one conversation. Each write is
     audited without its content.
  4. **Retention by last change.** A conversation is pruned `assistant.conversation_days` (30 by
     default) after it last moved, its messages with it, in the retention pass's bounded chunks.
- **Context:** An unattended run (an event rule's omnibot step, #2126) has no terminal to ask on. The
  2026-11-16 ruling keeps approvals client-side with no server-side pending approvals; this is how a
  paused run survives until its owner returns without the server holding a decision.
- **Limits:** Usage is totalled only when a client reports it, and the recorded provider reports none.
  A run killed between proposing a call and storing its marker leaves an open call with no marker;
  resuming it does not ask that call. The console panel is #1167.
- **Not chosen:**
  - **Scope-fenced conversations** (an admin reads anyone's). A transcript can hold what a person
    pasted; it is theirs like a draft, and the audit trail, not the transcript, is what an admin reads.
  - **Storing the proposal itself.** A stored proposal would carry the tool's policy and arguments as
    they were when it paused; rebuilding from the open call and the live tools means a policy
    tightened since is the one applied.
