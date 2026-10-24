---
title: "Legacy rows and stored declarations honor the one-writer ruling"
date: 2026-10-24
status: "Accepted"
refs: [1100]
summary: "What was written or declared before the one-writer ruling is held to it (#1100, amending ADR-0161): a declared pin is trusted only from the endpoint's audited `:retry` (marked `source` `key-pin`, legacy ones stamped from their audit row) and a node forgets a pin the worklist stops delivering; rows the ruling refuses are retired whole into `retired_sample` with a reason, never deleted and never flagged; stored driver specs and derived tasks never hand a node a refused emit, and the boot audit names them; a node's writer binds its task's component as well as its endpoint; contracts refuse reserved keys; and `command-lifecycle` (`command-issued`) is a reserved class"
---

- **Date:** 2026-10-24 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/), [metrics and properties](/architecture/properties/), [collection](/architecture/collection/), [api](/architecture/api/), [glossary](/architecture/glossary/), [drivers](/guides/admin/drivers/)
- **Decision:** (#1100, approved in session on 2026-10-24, amending
  [ADR-0161](#adr-0161-every-key-has-one-writer-class-held-at-the-storage-gateway).) The ruling held
  every new write; this holds what was written or declared before it, wherever it would still act.
  1. **A pin's provenance is its path.** A declared pin row (an ssh host key, a tls certificate's
     public key) carries `source` `key-pin`, which only the endpoint's audited `:retry` writes, and
     the worklist, the fingerprint, the reachability read and the pin a trust replaces read a
     declared pin by that mark alone. A pin `:retry` wrote before the mark existed is recognized by
     the audit row its transaction wrote (the same endpoint, the same key, the same instant) and
     stamped by a one-time backfill; any other declared pin row is retired, with the observed rows
     of the same key a node published under it and never before it (ordered by arrival, never by a
     node's clock). The endpoint then pins the key its node learned before, or trusts on first use
     again, under the gate as ever, and a retired pin's key the node did not learn before the plant
     is never taken as the pin from the observed series again (a node still holding it may
     republish it) until a `:retry` declares it; the boot audit names an endpoint left pinning
     nothing that way. A node forgets a pin the worklist delivered and then stops delivering (it was withdrawn):
     it carries and republishes only a key it learned on first use itself, and a session held under
     a withdrawn pin redials. A declared row the plain property route left, a clear among them, is
     not honored: the pin in force is the last one `:retry` set, which fails closed (a component
     whose key moved since reads `rejected` naming both keys, and one `:retry` trusts it).
  2. **Retire, never delete, never flag.** A sample or event row of a reserved key whose class
     would refuse it today is moved whole into `retired_sample` (its lane, its id, the row as it
     was, and the reason) by the backfill. A flag column would need every read to honor it, and a
     read that forgot would serve the row again, the drift the gateway invariant exists to prevent;
     moving the row leaves no read to forget. ADR-0080's floor (a prune never deletes a declared row
     or a series' latest) governs retention, the loss of true data with age; retirement removes from
     service a row the ruling says was never its writer's, and keeps the record, so the floor's
     reason (nothing true is lost) holds. The backfill's down restores every row it retired.
  3. **What retires.** A node-probe row on an owner that is not a component, written by a writer
     other than the node (a declared, intended or calculated row, a derived event, or a command's
     reply, which carries the instant its command executed), under an instance that names no
     endpoint of its component, or dated more than five minutes past the database clock (the cap new
     rows obey); a `health` row the rollup did not write; a `command-issued` event no command points
     at; and the pins of point 1. An event row another row references (a command's caused event, a
     derived event's source) stays. At boot, every event rule series a retirement took rows from is
     judged again, once, so a condition, and the alarm with it, that a retired row raised lets go;
     a series that cannot be judged is logged and left to the reconcile pass.
  4. **Stored specs and tasks never reach a node with a refused emit.** The worklist strips from
     every driver task an emit whose key the node lane may not carry (a reserved key, a calc rule's
     output) and keeps the rest, logging each once; a re-derivation from a stored spec never bakes
     one; and the server's boot audit names every stored driver spec, derived task, contract line,
     event rule and command type the ruling refuses, so an operator can mend it. Each is inert where
     it would act before it is named, and a rule whose clear event the ruling now reserves still lets
     its condition go, writing no event.
  5. **A node's writer binds both.** The node lane's writer names the component its task collects
     for as well as the endpoint it ran against, and the gateway refuses every row of a batch that
     lands on another component, under any key.
  6. **Contracts refuse reserved keys.** A product, standard or location-type contract line on a
     reserved key is refused on the operator's path and the seed's (a 422 naming the key), and a
     line stored before the refusal lends the key no default in the effective reads.
  7. **A command's lifecycle is a reserved class.** `command-lifecycle` holds `command-issued` and
     admits a command alone, so no push, node, driver or event rule puts a command on a timeline.
- **Context:** A write-side invariant shipped over existing data rules only what is written next.
  Rows and declarations from before it kept acting where nothing re-read them against it: a
  declared row the pin reads still took, a forward-dated row still read as the latest, a stored
  spec still compiled into a node's tasks, and a contract default still resolved as a value. The
  rows themselves could not always say who wrote them, so the pin's path became a mark on the row,
  recovered for history from the audit trail that already recorded it.
