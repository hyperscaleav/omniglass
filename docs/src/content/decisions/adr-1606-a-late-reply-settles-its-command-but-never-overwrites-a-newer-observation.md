---
title: "A late reply settles its command but never overwrites a newer observation"
date: 2026-11-15
status: Accepted
summary: "A response-settled command's reply is recorded as an observation of its target only when nothing was observed after the command's latest delivery to a node; otherwise the reply still settles or fails the command (its evidence is valid) and records nothing. Amends ADR-0141's recording of the reply."
refs: [1606, 1176, 141, 108]
---

- **Date:** 2026-11-15 | **Status:** Accepted | **Pages:** [commands](/architecture/commands/)
- **Decision:** (#1606, amending [ADR-0141](#adr-0141-a-command-settles-when-its-evidence-arrives-from-its-reply-first-and-observation-second)'s
  recording of the reply.) A reply read through a `from: response` binding is evidence for the command
  and is always judged against its intent. It is also recorded as an observation of a property target
  only when the target's latest observation is not newer than the command's latest delivery to a node
  (`command.dispatched_at`), the moment its exchange with the device began. A newer observation holds
  the device's later state, so the reply, however late it reaches the server, leaves it as the latest
  read; the command still settles or fails on the reply. The existing rules stand: the value is held to
  the property's validation, and it is written only when it differs from the latest observed value.
- **Context:** The reply was written at the database's now and compared to the latest observed value
  by equality alone, so a reply older than the latest observation still became the newest row. Seen
  while fixing #1176: `set-input hdmi3` to a udp panel whose exchange retried through 30 % loss; the
  panel pushed `hdmi3`, then `dp1`, and the retried reply (`INPUT hdmi3`) settled the command and wrote
  `hdmi3` over `dp1`, so the console showed an input the panel no longer had until the next poll.
- **Alternatives:** Stamping the reply with the time the node completed the exchange (the execution
  report would carry a node timestamp, as polled observations carry their sample time) keeps the reply
  as history, but the report has no such field, the node's clock is not the database's (ADR-0108), and a
  reply row behind a newer push is a state the device had only between two of its own writes. Leaving
  it for the next poll keeps a wrong value in the store for up to a poll interval.
- **Consequences:** The observation's time is the one it carries, a node's sample time for a poll and
  the database's for a push, compared to the database's delivery time. A node clock ahead of the
  database's can withhold a reply observation, which the next poll restores; one behind it leaves the
  earlier behavior, so the change is never wrong, only sometimes late (ADR-0141). Observation-settled
  commands are unaffected.
