---
title: "Every key has one writer class, held at the Storage Gateway"
date: 2026-10-24
status: "Accepted"
summary: "Every metric, property and event type has exactly one **writer class** (`node-probe`, `platform`, `calc`, `collected`), resolved from code and the catalogs, never stored, and the Storage Gateway refuses any sample or event write whose writer the class does not admit: an invariant beside scope injection, not a per-route check. The reserved system family (`node-probe`, `platform`) is declared in code and closed to operator-created types by name; a node-probe key reads only as the node's observed series, its time capped at the database's clock, and health only as the rollup's; fusion reconciles sources within the `collected` class; no reserved prefix (#1094 decides one for its platform keys)"
---

- **Date:** 2026-10-24 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/), [identity and access](/architecture/identity-access/), [metrics and properties](/architecture/properties/), [collection](/architecture/collection/), [calculations](/architecture/calculations/), [api](/architecture/api/), [glossary](/architecture/glossary/)
- **Decision:** The architect's ruling of 2026-10-24 (#1084, #1085, #1086): there is no path by which
  anything but the node writes what the console reads as the node's observation, and the rule is
  structural rather than a check each route remembers.
  1. **One writer class per key.** Every metric, property and event type has exactly one writer
     class, resolved from its name and the catalogs and never stored (`internal/writerclass`; read
     beside the type as `writer_class`): `node-probe`, the keys a node's own probes and session
     machine write, declared in `internal/collection/probekeys` (`collection-failed` among them
     now); `platform`, the keys only the Omniglass process itself writes (`health`, the rollup's
     verdict); `calc`, a key a calc rule names as its output; and `collected`, every other key.
     `node-probe` and `platform` are the **reserved system family**: fixed, internal, declared in
     code. The self-monitoring follow-on (#1094, the server, nodes, the bus, the database and the
     workers as components with built-in metrics only the binary writes) is the planned consumer of
     `platform`, which takes its keys without restructuring. **Amended (#1100):** a third reserved
     class, `command-lifecycle` (`command-issued`, a command's alone), joins the family, and what
     was stored before this ruling is held to it
     ([ADR-1100](#adr-1100-legacy-rows-and-stored-declarations-honor-the-one-writer-ruling)).
  2. **Each class admits a fixed set of writers**, the entry point a write comes through, each
     fixing the provenance it writes. `node-probe` admits the node lane alone, bound to the
     endpoint whose task the node ran; `platform` the process alone; `calc` the rule that outputs
     it, and an operator's declared value (which resolves ahead of the calculated one, by design);
     `collected` every observer (a driver's emits on the node lane, a push, a command's reply), a
     command's intended value, an operator's declared value and an event rule's event. The one
     declared row the reserved family admits is a key pin (the ssh host key, ADR-0157, and the tls
     certificate's public key, ADR-0160), set only through an endpoint's audited `:retry`. Operator and command are writers, not classes: a
     declared or intended row is never an observation, so it contests no observer.
  3. **Held at the Storage Gateway, as scope is.** Every gateway function that writes a sample or
     an event row names its writer and holds every row to its key's class before anything lands;
     a refusal is `ErrKeyWriterRefused`, a 422 naming the key and who writes it. A source guard
     fails on any function that writes a sample or event row without being a registered write path
     that runs the check, a table drives every registered path with a writer its key does not
     admit, and a migration that writes sample rows must be ruled as well. The declaration points
     refuse first with the same ruling: a command type's target, a driver spec's emits (and the
     node's interpreter, for a task derived before the refusal existed), a calc rule's output, an
     event rule's fire and clear types, and an operator-created type's name on every lane.
  4. **A node-probe key reads only as the node's observation.** The reads the reachability panel,
     the session pill and the ingest's transition guard compose over serve the observed series
     only, so no other provenance beside it stands in for the node's.
  5. **Fusion fits inside one class.** Same-key, multi-source reconcile is between the
     perspectives of the `collected` class (a driver's emits and a push, told apart by `source`):
     a writer class is the trust boundary, a source the sensor path within it. A reserved key has
     one observer and never fuses.
  6. **No reserved namespace.** Existing keys keep their names. The reserved family's exact names
     are closed to operator-created types on every lane, which the class resolution needs and a
     prefix would only repeat. A prefix buys one thing the invariant does not: names an operator
     cannot already hold when a later release ships a new reserved key. Today such a collision is
     not clean: the seed's authoritative upsert takes the operator's type over, so its later writes
     are refused naming the new writer, but the rows it wrote before read as the reserved key's.
     #1094's platform metrics are the likely case, so it decides the prefix (or a seed that refuses
     the collision) before it ships one.
  7. **A node's clock cannot outlive its tenure.** A node-probe sample dated more than five minutes
     past the database's clock is stored at the database's now, so a node cannot hold its account
     of an endpoint (or the gate and pin the worklist reads back) current after it stops holding
     the endpoint. Health reads serve the rollup's calculated rows alone, the platform's version
     of point 4.
- **Context:** The push lane's refusal of native keys (#1080) was one route's check on one surface.
  The other surfaces that land a sample (a declared value, a command, a driver's emits, a calc
  rule's output, the node lane itself) each answered who may write a key for themselves, or not at
  all, and `collection-failed`, declared outside probekeys, stayed outside the ruling (#1086). The
  calc rule's ownership of its output was the one structural precedent (a calc rule may not write a
  key a collection writes), with its ingest-side half left open. Generalizing that one primitive to
  every key, and holding it where every write already passes, closes the class rather than its
  instances. The split follows the code: a driver's emits and a push are one class because the
  shipped drivers emit `video-input` while a push may write it too, a command targets it, and the
  properties page's fusion exists to reconcile exactly that.
