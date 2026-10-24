---
title: "The catalog surfaces finish"
date: 2026-08-06
refs: []
form: bullet
---

- **The catalog surfaces finish**
  ([#601](https://github.com/hyperscaleav/omniglass/issues/601)). The arc-closing epic behind the
  five-lane split: every catalog page names one thing and works for the principals who may see it,
  every capability the lanes landed storage-deep gained its authoring surface, every schema the
  catalogs accept means what it says, and the wire rulings left in an issue body became decision
  log entries. Six slices, one rollup PR.

  In slice order: **the Types split** (#598) broke the one page holding two registries into
  Location Types and Secret Types, fixing the live viewer break (the shared data layer threw when
  the `secret:read`-gated fetch refused, taking the location tab down with it) and renaming the
  `type` permission resource to `location_type` on every surface (ADR-0082). **The metric
  contract surface** (#600) mirrored the property contract for the metric lane: twelve storage
  functions with in-transaction audit rows, three route families with the official-classifier
  refusal, the `ContractEditor` metric lane, the effective-metric resolution reading the metric
  series, and the missing `metric_type_id` indexes. **The command target on either arm** (#596)
  closed the whole projection, not just authoring: the read body, CLI, and console had been
  dropping the metric arm on the floor, and the seed loader had been silently ignoring or NULLing
  targets; both now refuse or carry, the exclusive-arc refusal surfaces as a named 422, and the
  console authors either arm by name through one picker. **Schemas mean what they say** (#595)
  landed the bare-`required` checker at nine storage sites (the mapped six plus three boot
  upserts the recount found) and made `params_schema` enforced at issue time; chasing the
  review's finding exposed the deeper defect, a stored object-valued `additionalProperties`
  arriving as a plain map the validator silently skips, fixed by normalizing it into a real
  sub-schema at parse so enforcement is real. **The guides** (#599) shipped Events and Commands
  admin pages with every claim required true on the branch (four softened where the code lacks
  the feature), and swept the dotted-name copy class wider than its map: console hints, doc tags,
  web comments, and the architecture corpus still teaching the retired two-rule design, with a
  docslint denylist entry so the class cannot return. **The wire contract** (#602) recorded
  ADR-0081: the `og.v1.<verb>.<node>` grammar, the `api.telemetry` own-segment ruling with its
  rejected reserved-name alternative, per-record subjects rejected, and the worklist/heartbeat
  singleton consequence with its HA fork deferred (telemetry does not face it; its durable
  JetStream consumer already joins), with messaging.md gaining the grammar section as
  page-of-record.
