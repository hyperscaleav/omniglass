---
title: "A driver declares its terminal replies, and a transport classifies its own refusals"
date: 2026-10-23
status: "Accepted"
summary: "Non-retryable errors are defined in drivers (#1049, the architect's ruling): a spec's `session.terminal` lists the replies retrying cannot fix, each a match with a class (`auth`, `host-key`, `certificate`, `protocol`, a code-held set), applied to the login and to every answer, refusal and arm reply; a transport classifies its own protocol refusals into the same classes. The primitive is `collection.TerminalError{Class, Reason}`; anything unclassified stays retryable, and a terminal poll fault waits its interval rather than the failure backoff"
---

- **Date:** 2026-10-23 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [glossary](/architecture/glossary/), [drivers](/guides/admin/drivers/)
- **Decision:** The architect's ruling, made in session on 2026-10-23 (#1049): non-retryable errors
  are defined in drivers.
  1. A spec's `session.terminal` lists the replies retrying cannot fix, each a `match` (a prefix
     or a regex) with a `class`. The classes are a code-held set (`auth`, `host-key`,
     `certificate`, `protocol`), refused at write when unknown, grown in code as transports need.
  2. A rule applies to every line the component sends the session's way: the login's lines, every
     exchange's answer and declared refusal, and every arm reply. A match fails that exchange with
     the refusal and rejects the session ([ADR-0155](#adr-0155-auth-is-a-layer-that-gates-a-held-session-and-a-terminal-refusal-reads-rejected)).
  3. A transport classifies its own protocol-level refusals into the same classes (an ssh auth
     failure is `auth`, a changed host key `host-key`), so a driver need not declare what the
     protocol already says.
  4. The primitive is `collection.TerminalError{Class, Reason}`, the reason the component's own
     text; anything unclassified stays retryable and follows the backoff and budget. A stateless
     or session poll whose fault is terminal waits its interval, never the failure backoff; one
     its session refused runs at the first tick that session is up again.
- **Context:** Only the driver knows what its component's refusal looks like ("auth rejected",
  "Login incorrect", an "account locked" reply), and only the transport knows its protocol's
  refusals; a session machine guessing from text would misclassify both ways. A fixed class set
  keeps an event rule's vocabulary stable across drivers.
