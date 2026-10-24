---
title: "A uuid is the address, a dotted path is a positional lookup"
date: 2026-08-08
status: "Accepted"
summary: "A uuid is the address the platform generates; a dotted path (location segments, a `$comp`/`$sys`/`$role` accessor, plane-local segments) is a human-typed positional lookup, resolved by an allowlist name rule that renders as a CLI argument, a REST path, or a NATS subject with no escaping; dash and bare renders are display-only and never accepted back. Extends ADR-0062, amends ADR-0076 in justification"
---

- **Date:** 2026-08-08 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [storage](/architecture/storage/), [identity and access](/architecture/identity-access/),
  [API](/architecture/api/), [glossary](/architecture/glossary/)
- **Decision:** An entity has two resolvable references, different guarantees, both legitimate and
  labelled as such. The **uuid is the address**: immutable, surviving rename and move, the only
  reference the platform itself ever generates or persists. The **dotted path is a positional
  lookup**: human-typed, resolving to whatever occupies that position now (`boi.17c.415a.$comp.display-1`
  after a panel swap resolves to the replacement, which is the point of a positional reference, not a
  defect of one). The containment rule: a reference the platform **generates** is always a uuid; a
  reference a human **types** may be a uuid, a bare name, or a dotted path. The console persists and
  addresses every read and write by `n().raw.id` (`web/src/pages/Components.tsx`, `Systems.tsx`,
  `Locations.tsx`), so a stale path can only ever exist where an operator typed one, into a CLI, a
  runbook, or a hand-built request. Because a platform-owned name recomputes at every `:move`
  ([ADR-0090](#adr-0090-a-derived-value-is-a-default-that-tracks-until-touched)), a path built
  entirely from platform-owned segments cannot itself go stale between the read that produced it and
  the write that consumes it; only an operator-owned segment can drift, and only because an operator
  chose to rename it.

  **This does not reopen [ADR-0079](#adr-0079-five-telemetry-lanes-and-property-stops-being-the-genus)'s
  one-name-rule collapse (#586).** That decision killed a NAME that could itself contain a dot
  (`icmp.rtt-avg`, a keyspace catalog key); today every name, keyspace or entity, is exactly one kebab
  segment, no dots, at most 100 characters, on the one rule `storage.ValidateName` applies everywhere
  (`internal/storage/name_rule.go`). What this decision adds is a **reference**, syntactically distinct
  from a name, that concatenates several individually-valid single-segment names with `.` and a
  `$accessor` (`$comp`, `$sys`, `$role`) into one positional path. `icmp.rtt-avg` was the last name a
  table's `name` column held with a dot in it, backfilled dot-free by #586 itself
  (`icmp.rtt-avg` to `icmp-rtt-avg`); no name column has held one since, and this epic adds none: a
  dotted value is never stored, only resolved.

  **The allowlist is the load-bearing fact.** The entity name rule, `^[a-z0-9][a-z0-9-]*$`
  (`internal/storage/name.go`), is an allowlist, not a denylist of characters someone remembered to
  exclude. A name can never contain a separator or wildcard from any protocol, chosen or not yet
  chosen: `.` (path segments), `$` (accessors), `*` and `>` (NATS), `/` and `#` (MQTT), `%` (URL
  escaping), or `:` (reserved by the router for a custom method's verb suffix, `POST
  /components/{ref}:rename`: a name admitting `:` would make `rm215a:rename` ambiguous between the
  entity `rm215a:rename` and the entity `rm215a` with the `:rename` verb, so the allowlist excludes it
  for the same reason it excludes everything else on this list). That single property is what lets one
  grammar render as a CLI argument, a REST path segment, or a NATS subject with no escaping anywhere,
  and lets most segments render as a DNS label or an email localpart, since the character set alone is
  a subset of both: what the allowlist does **not** guarantee is a segment's fit under DNS's own
  63-octet label ceiling (the 100-character entity limit is wider) or DNS's ban on a trailing hyphen
  (`^[a-z0-9][a-z0-9-]*$` legally admits `abc-`); both are a render-time concern for whichever segment
  eventually feeds a hostname, not a naming rule this decision enforces. An allowlist still composes
  across every namespace it has not met yet, where a denylist has to be re-audited for each new one.

  **A percent-encoded slash arrives already decoded.** The HTTP handler decodes a path parameter
  before the address parser ever sees it (verified against the router, not assumed, before Task 12
  built on it), so a caller cannot smuggle an extra path level past `ParseAddress` by encoding a `/` or
  a `.`. Every segment, in the location root, the plane tail, and the role name alike, passes the
  entity name rule inside `ParseAddress` itself (`internal/storage/path.go:74-77`) before any of it
  reaches a query: validation is structural, not a property of what happens to already be in the
  database.

  **A dash render and a bare render are display-only, and neither is a form the resolver ever treats
  as a path.** `RenderDash` (`boi-17c-216b-display-1`) strips the accessor; `RenderBare`
  (`boi17c216bfp1`) further compacts the final segment to the component type's `abbrev` plus its
  ordinal and drops every separator, hyphens included (`internal/storage/render.go`). Both exist for
  labelling only (a cable tag, an asset sticker, a compact row sub-line): accessor-stripping is lossy
  for the dash form and stem-compaction is lossier still for the bare form, so neither round-trips
  through `ParseAddress`/`resolvePath`. Both also still satisfy the entity name rule on their own
  (letters, digits, and hyphens, no dot or `$`), so a dash or bare string handed back to the resolver
  is not refused as malformed: `ParseAddress` reports it is not an address at all and it falls through
  to the ordinary bare-name path, almost always matching no row, an unremarkable 404 rather than a
  distinguishable error.

- **Decision (six edges, recorded rather than hidden):** the read path (`scopedByNameInScope`,
  `refPolicyHide`) and the write path (`resolveScopedRef`, `refPolicyForbid`) share one primitive,
  `resolveRef` (`internal/storage/scopedcrud.go:521-546`), but they do not share every guarantee, and
  the gaps below are shipped as stated limits, not silently left for the next reader to discover.

  1. **`resolveRef`'s write-path policy proves "writable here," not "readable here."**
     `resolveScopedRef` (`scopedcrud.go:633-649`) narrows candidates by the caller's create- or
     action-scope, not its read scope. A resolved reference is one the caller may place a binding
     against, which is a narrower claim than the scope ruling's own wording ("scope decides before
     ambiguity does") suggests on its own. **Amended (#700):** that claim was too weak for a
     CROSS-TIER placement reference, and the amendment narrows which references the edge covers
     rather than changing the policy itself. A create's or a move's location and system references
     (`CreateComponent`, `CreateSystem`, `MoveComponent`, `MoveSystem`) resolved existence-only,
     because the caller's create scope is resolved for the entity being written and can never match
     the referenced tier's own ancestor chain. Once
     [ADR-0100](#adr-0100-a-label-cascades-where-the-blast-radius-is-a-placement-and-waits-for-the-verb-where-it-is-the-estate)
     put placement into the label data map, the label those writes stamp and hand back is rendered
     from the referenced row's label, so "writable here" stopped being the right question and
     "readable here" became it. They now go through `resolvePlacementRef`, which applies the READ
     path's policy (`scopedByNameInScope`, `refPolicyHide`) using the caller's scope on the
     REFERENCED tier, matching what `:renderLabel` already did for the identical references
     ([ADR-0104](#adr-0104-a-create-form-shows-the-name-it-can-know-and-never-mints-one-to-preview-it))
     so a preview and the create it previews cannot disagree. `resolveScopedRef` keeps every
     same-tier parent and owner reference, where this edge still reads as written. **Amended again
     ([#705](https://github.com/hyperscaleav/omniglass/issues/705)):** `CreateNode` and `UpdateNode`
     bind a location the same way and were the one caller-supplied placement reference left
     resolving existence-only, so they take the same seam. The node tier reaches it by the plain
     invariant rather than by the label argument above: no node label rule exists, so the write
     stamps nothing from the location it names and discloses nothing about it, but ABAC scope is
     injected on every applicable query and a reference resolved without it is an exception to that
     rather than a carve-out anybody decided on. The refusal is the same non-disclosing
     `ErrLocationNotFound` an absent location gives.
  2. **A bare-name `forbidden` is a name-existence oracle.** A name matching at least one row, none of
     them in the caller's action scope, is `cfg.forbidden` (403), not the read path's non-disclosing
     404 (`scopedcrud.go:469-475`). A caller can learn a name exists somewhere in the estate from the
     status code alone. This predates the epic and is tested (`interfaces_scope_test.go:82`), and it
     is now asymmetric with the read path this branch tightened (`scopedByNameInScope` folds the same
     two cases into one 404), by design: several routes' contracts already depend on the 403/404 split
     a caller supplied its own reference into, which is not the new disclosure a read's uuid would be.
  3. **An ambiguous `?system=` on a component's tags read is a new, narrower oracle, preferred over
     what it replaced.** `ResolveTags` (`internal/storage/tags.go:449-459`) now 409s a `component:read`
     caller whose `forSystem` filter matches more than one system, redacting candidates
     (`withoutCandidates`). A caller with no `system:read` grant learns two systems share that name.
     This is a deliberate improvement: the alternative silently seeded the tag cascade from whichever
     system a bare-name lookup happened to resolve to, returning a wrong answer with no signal at all.
  4. **The `$role` accessor parses but resolves nowhere.** `AddressRole`
     (`internal/storage/path.go:35-42`) is syntactically real: `boi.17c.$sys.av.$role.primary-dsp`
     parses to a well-formed `Address`. `addressKindTable` (`scopedcrud.go:247-263`) has no table for
     it, so `resolvePath` reports the same `ErrPathNotFound` it would for any other plane mismatch, a
     non-disclosing 404 that does not distinguish "role addressing is not built" from "this role does
     not exist." Reserving the grammar ahead of its resolution is a stated decision here, not a gap
     found later: the day a system-role read exists, it inherits a grammar it never has to renegotiate.
  5. **The boot-seed reconcile-and-delete carve-out is a separate pattern, cross-referenced, not
     restated.** `choice_alternate`'s every-boot reconciliation
     ([ADR-0087](#adr-0087-capability-gated-staffing-retires-an-alarm-impairs-its-component-not-a-named-capability),
     [storage](/architecture/storage/#migrations-three-buckets-kept-separate)) is about which seeded
     rows a reboot may remove, not about how a reference resolves or who holds the pen on a value; it
     shares no mechanism with this decision or with [ADR-0090](#adr-0090-a-derived-value-is-a-default-that-tracks-until-touched)
     and is named here only so a reader does not conflate the two governance patterns.
  6. **The tier guard inside `resolveRef` is forward insurance, not proof.** `resolveRef` panics if the
     caller's scope was resolved for a resource that does not `Cover` the config being checked
     (`scopedcrud.go:521-524`). Every one of the 29 non-test call sites of `scopedByNameInScope` and
     `resolveScopedRef` today passes a `resource` label already derived from the same config it is
     checked against, so the guard **cannot fire on any
     input the current code produces**, and a green suite running with it live proves nothing beyond
     that (`scopedcrud.go:507-517`, in the code, says so explicitly). It is insurance for the next call
     site that copies a pattern without updating the label. It also inherits a blind spot from
     `scope.Covers`: for `secret`, `variable`, `field`, and `telemetry`, every tier is admissible, so
     the guard tells a right family from a wrong one but not a right tier from a wrong one within that
     family, exactly the shape a real cross-tier regression took in this epic's own review before a
     scoped test, not this guard, caught it.

- **Extends [ADR-0062](#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle):** that
  decision's dual-accept clause already said a reference "resolves whichever form it is given"; this
  decision makes the third form literal rather than aspirational. `loadByRef`
  (`internal/storage/scopedcrud.go:158-202`) tries a uuid, then a parsed dotted address, then a bare
  name, in that order, for every scoped tree entity.
- **Amends [ADR-0076](#adr-0076-a-renameable-human-typed-identifier-stays-in-the-url-and-the-write-returns-the-uuid)**
  in justification, not shape: `:rename` stays exactly the gated custom method ADR-0076 built, and it
  earns a second job under [ADR-0090](#adr-0090-a-derived-value-is-a-default-that-tracks-until-touched):
  it is now precisely the boundary where an operator takes the pen from the platform's name-tracking.
  What changes is why the ceremony still earns its keep. ADR-0076 accepted "an external reference held
  as a name breaks on rename, and nothing on the server can repair it" as the renameable identifier's
  cost. Once the uuid is the address that argument is repaired: an integration holding the id survives
  every rename, and one holding a path is a positional lookup honestly reporting whatever occupies that
  position now, not a broken link. What remains, and is sufficient on its own, is the permission split
  (an operator trusted to edit `display_name` is not thereby trusted to rewrite the identifier
  colleagues type) and the pen-taking act itself.
- **Context:** every estate name was globally unique before this epic, so operators hand-encoded the
  building into every name and the encoding went stale on every move, twice per room (location and
  system). ADR-0076 already made the id the durable reference and the name the renameable display key;
  this decision (identity-model epic [#627](https://github.com/hyperscaleav/omniglass/issues/627),
  Tasks 10 through 12 and 15) makes the id literally an address a caller composes, types, and resolves,
  and gives a human a second, positional way to reach the same row without inventing a third identity
  field. The path grammar was reserved, syntax-only, in `internal/storage/name.go` since ADR-0076;
  Task 10 made every internal owner-resolve id-based ahead of the placement-scoped uniqueness DDL
  (Task 11, `db/migrations/20260808090000_names_scope_to_placement.sql`), Task 12 built the parser and resolver
  (`internal/storage/path.go`, `resolvePath`), and Task 15 put the resolved path, its segments, and
  both renders on the wire and pointed the console at uuids exclusively. Task 13's `:move` verb
  ([ADR-0088](#adr-0088-a-placement-change-is-an-authorization-act-so-a-move-is-its-own-verb)) and
  Task 14's name generation ([ADR-0090](#adr-0090-a-derived-value-is-a-default-that-tracks-until-touched))
  are what make a platform-owned segment of a path trustworthy rather than merely typeable.
- **Tracked under** epic [#627](https://github.com/hyperscaleav/omniglass/issues/627).
