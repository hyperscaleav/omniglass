---
title: "A renameable, human-typed identifier stays in the URL, and the write returns the uuid"
date: 2026-08-04
status: "Accepted"
summary: "the name stays renameable and addressable; rename is a custom method, every write returns the uuid, and one validator applies one of two rules"
---

- **Date:** 2026-08-04 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/), [api](/architecture/api/), [storage](/architecture/storage/), [tags](/architecture/tags/), [config, secrets, and variables](/architecture/variables/)
- **Decision:** An entity is addressed by its **`name`**, a renameable identifier an operator types, as well as
  by its immutable **`id`**. A rename is an explicit custom method (`POST /<collection>/{ref}:rename`, gated by
  `<resource>:rename`) rather than a field write, and a write returns the `id` so a client can store the
  stable handle and stop depending on the name it used. References inside the platform store the `id` only
  (ADR-0056), and `audit_log.resource_id` keys on it, so a rename moves exactly one column. Two
  response bodies do not yet carry the id, `NodeBody` and `SystemRoleBody`, so a client of those two
  has the name and nothing else to diff; closing that is a follow-up, not a change of direction.
- **Decision (what the name rule became):** there is **one validator**, `storage.ValidateName(table, name)`,
  which picks the rule from the table's declared identity shape instead of from the call site; the platform's
  two other name rules are deleted, not renamed. Two rules survive rather than four, and they share one
  character set, differing only in the dot and the ceiling: an **entity name** is one segment of lowercase
  letters, digits, and hyphens, at most 100 characters (`hq-boardroom-dsp`); a **keyspace name** is a dot-joined
  path of those same segments, at most 128 (`icmp.rtt-avg`). Neither may be uuid-shaped. Only `property_type`,
  `event_type`, and `command_type` are keyspace. `tag`, `variable`, and `secret` had been declared keyspace on a
  claim nothing exercised, and move to the entity rule, since none of them ever carries a dot: a behaviour
  change on three shipped surfaces, so `cost_center` and `$var:crestron.ssh` stop being creatable and the docs
  that taught them say `cost-center` and `$var:crestron-ssh`.
- **Context:** this is a deliberate departure from prevailing practice, recorded so it is not mistaken for an
  oversight. Prior art runs the other way almost without exception: AIP-180 states that a resource must not
  change its name, Kubernetes `metadata.name` is documented "Cannot be updated", GCP `projectId` and Tag
  `shortName` are Immutable, and Azure's own guidance is that most resource names cannot change after creation
  and that details belong in tags instead. More pointedly, systems in this domain **retreated** from it after
  shipping: Grafana deprecated its title-derived dashboard slug in v5.0 and added `uid` because name-based
  references broke dashboards, and PagerDuty froze `CustomField.name` at creation and routes all renaming
  pressure to `display_name`. None migrated the other way. The case for keeping it is operator ergonomics in an
  estate whose entities are named after rooms and racks that genuinely get renamed, and the mitigation is
  Linear's: the typed identifier is renameable, the uuid is returned on every write, and a client that kept
  using the name can detect the move by diffing the id it holds. The cost is real and accepted: an external
  reference held as a name breaks on rename, and nothing on the server can repair it. Alias and redirect
  machinery is deliberately not built yet; it is the first thing to reach for if that cost shows up.
- **Amended by [ADR-0089](#adr-0089-a-uuid-is-the-address-a-dotted-path-is-a-positional-lookup)** in
  justification, not shape: `:rename` stays exactly this custom method, gains a second job as the
  operator's pen-taking act ([ADR-0090](#adr-0090-a-derived-value-is-a-default-that-tracks-until-touched)),
  and the accepted cost above is repaired rather than removed. Once the uuid is the address, an
  external reference held as a name no longer breaks on rename in the way this entry describes: an
  integration holding the id survives every rename, and one holding a dotted path is a positional
  lookup honestly reporting whatever occupies that position now. What remains, and stands on its own,
  is the permission split this entry already named.
