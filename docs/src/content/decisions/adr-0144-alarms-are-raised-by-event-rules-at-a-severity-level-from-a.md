---
title: "Alarms are raised by event rules, at a severity level from a ranked registry"
date: 2026-10-18
status: "Accepted; amended (#977): the dedup key names the rule by uuid, and a rule's name is renameable"
summary: "Alarms are raised by event rules (one table per rule kind, `rule:*` authoring, `raised_by_rule_id`, one alarm per endpoint) at a severity level from a ranked registry referenced by name; `critical` became `high`, the outage floor"
---

- **Date:** 2026-10-18 | **Status:** Accepted | **Pages:** [alarms and actions](/architecture/alarms-actions/), [health](/architecture/health/), [glossary](/architecture/glossary/)
- **Decision:** The detection half of ADR-0050 is built (epic #819), on five rulings the architect took
  at approval:
  1. **One table per rule kind:** `event_rule` now; `calc_rule` and `action_rule` join as siblings.
  2. **`rule:*` gates authoring**, one resource across kinds; reading raised alarms keeps its
     component-tier gates.
  3. **An alarm names the rule that raised it** in a nullable `raised_by_rule_id`, its `dedup_key`
     taken from the rule.
  4. **Severity is a registry of named, ranked levels**, referenced **by name** (a foreign key with
     `ON UPDATE CASCADE`). The design's `order` column is named `rank` (a reserved word otherwise). The
     three values ADR-0050 fixed map by meaning: `critical`, the outage word, becomes `high`, the outage
     floor of the ranked levels, so no verdict moved; a component's own verdict now reads the rank
     (`high` and above is an outage). The reference is added `NOT VALID` because the levels are
     boot-seeded after migrations run; it is enforced on every write. An official level never takes
     over an operator's (a clash stops the boot naming it), and re-ranking a level recomputes the
     health of every component with an open alarm at it.
  5. **One alarm per endpoint** for a per-endpoint input: the dedup key is the rule name plus the
     sample's instance, so a component with several endpoints holds an alarm per endpoint.
     **Amended 2026-10-21 (#977):** the dedup key names the rule by its uuid, not its name
     (`rule:<uuid>[:<instance>]`), since an event rule's name became renameable (as
     [ADR-0062](#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle) and
     [ADR-0076](#adr-0076-a-renameable-human-typed-identifier-stays-in-the-url-and-the-write-returns-the-uuid)
     require of a registry; the rules shipped create-only in #918). A rename keeps every open alarm,
     condition and streak attached; an event's `source_rule` stays the historical name; an official
     rule keeps its name. A one-time data migration rewrote existing keys, open and cleared.
  6. **Sustains are clocked by samples and a sweep** (ruled 2026-10-19, during #920): a durable streak
     per rule, component and instance acts once it has held for its sustain, on a later sample or on a
     server sweep every five seconds. The definition's thin cut had sustains clocked by samples alone,
     but the official reachability rule reads a verdict the node emits only on change, so a condition
     that stays down sends no later sample; the sweep (like the command settlement sweep, ADR-0141) closes
     that without the full durable timer (#419). The streak's clock is the server's receipt time, not
     the sample's timestamp, so the sample path and the sweep read one clock and a skewed node cannot
     waive a sustain. A reconcile pass once a minute on the same tick (#928) re-judges each series that
     took a sample since the pass before last and corrects a rule state that disagrees with its newest
     sample, healing a decision lost to an enable racing an in-flight batch or to a deadlock abort.

  7. **A rule's live state is its open alarms, its pending streaks, and when it last judged a
     sample** (ruled during #921, amended by #929). The definition's "last evaluated" first shipped
     deferred: a per-sample write was kept off the ingest path in the #920 review, and a read-time
     newest-sample has no supporting index on the property lane and is not the same fact (a disabled
     rule, or a sample the rule cannot evaluate, has a newest sample and no judgment). #929 built it as
     a coalesced stamp, `last_judged_at` in its own `event_rule_live` row: an ingest batch stamps each
     rule it judged a sample by once, as its last statement, only when the stamp is at least 5 seconds
     old, skipping (`SKIP LOCKED`) a row another batch is stamping. So the hot path pays nothing per
     sample and at most one statement per batch, batches never queue on a stamp, the value is honest to
     within 5 seconds on every replica (an in-memory per-replica value was rejected: it lies across
     replicas), and the rule row every batch holds `FOR SHARE` is never written by ingest. The severity
     levels surface beside the rules, and the console authors operator levels as the CLI and the API
     do (#930).

  Rules read like a registry (`rule:read`, any grant); writing one takes `rule:*` on an all-scope
  grant, since a rule's selector reaches the whole fleet, the same reasoning as a root create (ruled by
  the architect 2026-10-19). A rule's live state names components, so its counts, open alarms and
  streaks cover only the components the caller's component read scope admits. A severity level is
  judged the same way (ruled by the architect 2026-10-20, during #930): it reads like a registry
  (`severity_level:read`, any grant), but creating, updating or deleting one takes `severity_level:*`
  on an all-scope grant, since a level's rank moves health fleet-wide; a bounded holder is a 403
  (`ErrSeverityLevelForbidden`), and the console offers the writes on the caller's all-scope
  permissions only.
- **Context:** #606 left the rules vocabulary to the definition that builds it; #819's definition
  posed the five forks with the concrete DDL and the architect approved every recommendation
  (2026-10-18). ADR-0050 had fixed severity at three values with a CHECK; the registry supersedes that
  part of it.
