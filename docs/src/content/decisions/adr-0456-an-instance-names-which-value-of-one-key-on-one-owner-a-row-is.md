---
title: "An instance names which value of one key on one owner a row is, and an owner-level read reduces across every instance"
date: 2026-10-26
status: Accepted
summary: "`instance` has one meaning on every lane: which one of several values of one key on one owner a row is. Each provenance has one stamper: the observer for observed (the node lane's task endpoint or a driver's own instance, the endpoint a webhook arrives at, the instance a push names), the operator for declared (owner-level on every shipped route), the command for intended, and the empty instance for calculated and, deliberately, for health. A read asking about the owner reduces across every instance of the key; one naming an instance reads that series and never reduces. Reconciliation's is is worst-case and names its instance, told the newest intended; an un-instanced command is judged on the endpoint it was dispatched over, else on any instance agreeing; a current-value display shows the newest across instances and names it (#1020)"
refs: [456, 430, 1020]
---

- **Date:** 2026-10-26 | **Status:** Accepted | **Pages:** [metrics and properties](/architecture/properties/), [commands](/architecture/commands/), [api](/architecture/api/), [glossary](/architecture/glossary/)
- **Decision:** (#456, the architect's ruling; a sub-issue of #430.)
  1. **One meaning.** `instance` names which one of several values of one key on one owner a row
     is. Series identity stays `(owner, type, instance, provenance)`, and the empty instance is the
     owner-level series.
  2. **One stamper per provenance.** *Observed*: the observer. On the node lane, the endpoint whose
     task collected it (the ingest stamps the endpoint's name where a sample names none) or a
     driver's own instance (one series per port); on the webhook lane, the endpoint the post
     arrived at; on the push lane, the instance the sender names, else empty; a command's reply read
     as an observation lands on the endpoint it was dispatched over. *Declared*: the operator. Every
     shipped route (a component's, a system's and a location's properties) declares owner-level,
     the empty instance, and a per-instance declaration waits for a surface that addresses
     instances; the one declared row already written under an endpoint is a key pin
     ([ADR-0157](#adr-0157-telnet-and-ssh-are-held-line-transports-their-login-is-the-drivers-and-the-ssh-host-key-is-pinned-on-first-use),
     [ADR-0160](#adr-0160-tls-is-its-own-transport-over-a-shared-primitive-and-its-pin-is-the-public-key)).
     *Intended*: the command's instance, empty for an owner-level intent. *Calculated*: empty, since
     a calc output is aggregate. *Health*: empty, always and deliberately. An entity has exactly one
     health, so a health row never carries an instance, and no reader or writer should give it one.
  3. **How a read resolves.** A question about the owner (the empty instance) over an instanced key
     reduces across every instance of the key on the owner. A question naming an instance reads
     exactly that series and never reduces.
  4. **Reconciliation** (want, told, is): `is` resolves across every observed instance,
     worst-case. Drift is any observed instance disagreeing with want; `is` is then the newest
     disagreeing value, else the newest agreeing one, and the read names the instance it came from
     (`is_instance`). `told` is the newest intended value across instances. Want stays the
     owner-level declared cascade.
  5. **Settlement.** A command with an instance is judged on that series alone. An un-instanced
     command dispatched over an endpoint is judged on that endpoint's series (#900,
     [ADR-0141](#adr-0141-a-command-settles-when-its-evidence-arrives-from-its-reply-first-and-observation-second),
     kept). An un-instanced command no endpoint has carried is an owner-level intent, judged
     any-instance-agrees: settled when an observed instance agrees (at once when that observation
     was taken at or after the intent, else when the window closes), failed when instances were
     observed and none agrees past the window, and timed out when nothing was observed. An
     observation on any instance runs its settle check, and the computed verdict reads the same
     observation the recorded outcome did.
  6. **A current-value display** (a component's vitals row, #1020): the newest value across
     instances, naming the instance it shows. Recorded here; #1020 builds it.
- **Context:** The writer and the readers disagreed. The ingest stamped every node-lane sample with
  its endpoint's name, but reconciliation read `is` and `told` only at the empty instance, so a
  property collected through an endpoint could never appear as `is`, and drift was computed against
  a value that could never arrive. Settlement read the empty instance too, which #900 had fixed only
  for a command a node had already carried, and the verdict a command's issue answered with kept
  reading the empty instance even then. The break was latent while nothing collected a declared
  property, and live the moment a driver does (the seeded switcher drivers emit `video-input`, which
  `set-input` targets). One meaning for the column and one resolution rule for every reader, rather
  than a patch per reader, keeps the next one (the vitals row, #1020) from repeating the split. The
  authored form, a `name[instance]` suffix in an extract spec, stays design under #430.
