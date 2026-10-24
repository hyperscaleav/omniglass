---
title: "A held line session routes every inbound line, listeners first, answers by reply shape"
date: 2026-10-23
status: "Accepted"
summary: "Listeners run over the held session (#817): `arm` commands on every connection before up (plumbing, self-logged), `match` claims a pushed line, its emits land as push samples under the listen task. Every inbound line is routed listeners first, then the exchange in flight by its declared `request.reply` shape (or the first unclaimed line), then unclaimed (counted; `unmatched: collection-failed` lands one fault per tick). Commands ride the held session. Reverses the designed matcher order (outstanding poll first)"
---

- **Date:** 2026-10-23 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [nodes](/architecture/nodes/), [drivers](/guides/admin/drivers/)
- **Decision:** A driver's listeners come alive over the held session (#817), and one reader per
  connection routes every inbound line:
  1. **Arm on every connection.** A listener's `arm` commands are sent in order on each connection
     before the session reads `up` (establish and every recover). They are plumbing, per the #603
     ruling: a node self-log with the device's reply, never a command row. The ack is the first
     line of any kind, one a listener claims included (both have it), since a device may ack a
     subscribe with the push itself; no ack within the timeout is logged at warn ("armed,
     unacknowledged") and does not fail the attempt, while the connection dying during the arm does.
  2. **Listeners first.** Listeners are offered a line in function-name order; the first whose
     `match` picks it claims it, and its emits land at once as samples under its listen task. A
     claimed line is never an exchange's answer, unless the exchange declared a reply shape the
     line matches, when both have it (a protocol whose answers look like its pushes still answers).
  3. **Answers by reply shape.** A request may declare `reply` (a `prefix` or `regex`, the regex
     Go RE2 with substring semantics, so anchored to mean a whole line); the exchange in flight
     takes the first line it matches, or, with none declared, the first line no listener claims. A
     reply may also declare an `error` shape: an unclaimed line matching it answers the exchange as
     a refusal carrying the device's text, never a timeout. A shaped exchange that gets no answer in
     time fails alone, keeping the connection and not charging the session, since shape routing
     cannot misroute its late answer; an unshaped one still recycles (ADR-0150). A line begun
     before the request was written is never its answer.
  4. **The rest is unclaimed.** Counted and self-logged once per tick; a spec declaring
     `unmatched: collection-failed` (default `discard`) also lands one `collection-failed` per tick.
  5. **Commands ride the session.** A command delivery names its endpoint and carries its binding's
     reply shape; a node holding a session there actuates over it, falling back to a throwaway
     connection only when none is held. A refusal reports the device's text as both the execution
     error and the reply, so the command fails at once with its reason (a response-settled binding
     fails on the execution error before it reads the reply).
  6. **Patterns compile once.** Every match, reply and extraction regex is compiled once per
     process and reused by the reader, and refused at write when it does not compile.
- **Context:** The nodes page designed an ordered matcher set trying an outstanding poll's response
  matcher first. On a line protocol that order lets a push that happens to fit a loose answer (or
  any line, when the poll declares no shape) be eaten as the answer, which loses a sample and
  misreports a poll; a listener's rule is the more specific statement of what a line is. Declaring
  reply shapes closes the window ADR-0150 point 5 left open for every request that declares one;
  a request without one still takes the first unclaimed line, so a driver whose protocol mixes
  unsolicited lines with answers declares its replies (the seeded `newtron-nvp` does).
