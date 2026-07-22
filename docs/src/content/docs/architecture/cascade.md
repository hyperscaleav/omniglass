---
title: Cascade
description: How effective settings (config, variables, tags, rule-sets) are resolved for any entity and how the resolve view explains why a given value won.
sidebar:
  badge:
    text: Design
    variant: caution
---

The cascade lets an operator set a value once, high up, and have it apply everywhere below while still being overridable on any one entity, and then explain exactly why a given value won. It resolves the effective settings (config, variables, tags, rule-sets) for any entity.

## What it resolves

The effective **config and variables** ([config and credentials](/architecture/variables/)), **tags**, and **rule-sets** for any entity. A
first-class **resolve view** explains every effective value: the winning source
and what it shadowed. The order is deliberately hand-tuned (not derivable from a
single rule), so the resolve view is the safety net, not an afterthought.

## User stories

What the cascade is for, in operator terms:

- **Set once, override where it matters.** I set the standard poll interval on the
  Room Kit template, but HQ needs its devices on a different credential, so I set it
  at the HQ location and it overrides the template for everything there, no template
  edit; a room three levels down just inherits the nearest setting. *(structural
  chain: deployment beats template defaults; deepest location wins)*
- **Composition tightens the part.** My "Standard Huddle Room" template polls codecs
  every 30s, tighter than the codec's own 60s default, and every codec placed in a
  huddle room picks that up. *(system_template beats component_template)*
- **"Why did it get this?"** RM204 polls every 5 minutes and I don't know why; the
  resolve view shows it is the "Old-firmware Room Kits" group (weight 450) shadowing
  the template's 30s. *(the effective-config resolve view)*

The cross-cutting cases (a fleet-wide fix that auto-clears, a broad policy as a floor, a hand-picked
set) are the [group](/architecture/groups/) stories, where the group is placed by weight on this same
scale.

## The structural chain

General defaults to specific deployment; most-specific (deepest) wins:

```text
global                fixed
component_template    the leaf entity's template defaults
system_template       the leaf's owning-system template defaults
location tree         campus -> building -> floor -> room      (deepest wins)
system tree           parent system -> subsystem -> ...         (deepest wins)
component tree         chassis -> card -> ...                    (deepest wins, = the leaf)
```

- Resolution runs **for the leaf** over its containment path; a non-leaf entity
  (a chassis, a floor) resolves over its own shorter subset.
- **Philosophy: deployment beats type/template defaults.** HQ's location credential
  overrides the Room Kit template default; "Standard Huddle Room" overrides the
  bare Room Kit default.
- **Templates are the leaf's base.** Ancestor nodes in the trees contribute their
  **instance** bindings, not their templates (a chassis hands a card its
  chassis-wide credential; the card keeps its own template).
- The three structural segments are **variable-depth trees** (parent-reference
  nesting, arbitrary depth); the deepest node wins. Weight-free, pure depth.

## Combinators (by what is resolved)

- **config / variables -> scalar override**: the deepest/highest source wins; one value.
- **tags -> union on name, override on value**: names accumulate; for a given
  name, the winning source's value wins.
- **rules** (`calc_rule` / `event_rule`) -> **additive
  accumulation + explicit suppression**: a leaf is governed by the union of rules
  from every layer; a layer removes one by name with a suppression.

## Groups overlay the cascade, placed by weight

The structural tree handles config by position and kind. **[Groups](/architecture/groups/)** handle
config by attribute or by a hand-picked set, cutting across the tree. The cascade does not define
groups (see [groups](/architecture/groups/) for the primitive); it consumes them by **weight** on the
one specificity scale.

**One specificity scale.** Structural layers auto-derive a specificity from position, weight-free, the
operator never tunes it: `global` lowest, then the templates, then the location / system / component
trees by depth, then the entity's own **instance** at the ceiling. A **group's weight is its
specificity on that same scale**, so a group sits wherever its weight lands relative to the structural
bands: a high weight beats deployment (a must-apply override), a low weight loses to it (a default that
deployment overrides). The instance ceiling beats any group; equal specificity breaks by creation
order. A typed group applies at its own level: a component-group to components directly; a system-group
reaches a component **through the system layer** of its cascade.

So however many groups an entity is in, the group band collapses to one weighted list on the shared
scale, fully predictable, and the resolve view names the winner.

**The comparison key is segmented, not a single number.** Precedence is a lexicographic key
`(segment_rank, depth, group_weight, creation_order)`, compared field by field in that order. The
`segment_rank` orders the structural bands (global, templates, location / system / component trees, the
instance ceiling); `depth` orders within a variable-depth tree (deepest wins); `group_weight` places a
group relative to the structural bands; `creation_order` breaks an exact tie. Because the segment is the
first field, a structural segment never overruns into another regardless of how deep a tree runs or how
many group weights stack: a deeper node or a heavier group raises a later field, never the leading
`segment_rank`. The single specificity numbers shown elsewhere on this page (e.g. `0`, `100`, the `300s`,
the `400s`) are a **presentation-only** flattening of that key for the resolve view, not the comparison
key itself.

A **`_type`** (device/app, AV-System, room) is not a cascade layer: it is a classification attribute,
resolved by a [group](/architecture/groups/) filter (`type == X`) placed by weight, never a tree
position. The tree is structural; attributes are groups.

## The registry is outside the cascade

`datapoint_type` defines **identity** (kind, unit, validation, fusion_policy)
for every datapoint key, which the cascade never overrides (policy, not ontology).
Ship-with **default policy** lives at `global`, the floor of the chain.

## Structural multi-membership (a component in N systems)

Distinct from group membership. A component belongs to zero or more systems through
[`system_member`](/architecture/core-entities/#membership-what-a-role-attaches-to),
and the resolution **takes the system to resolve against**: given one, it resolves
against that system, and only if the component is a member of it, since naming a
system it has no binding to must not lend it configuration. That is the "config
differs per system" case, answered as a **per-system effective view** computed on
demand rather than by merging chains.

Asked with **no system in hand**, the chain is seeded from the component's
**primary** membership. That is what `is_primary` is for, and it is the whole of
what it is for: a default for context-free callers, never a rule that overrides a
caller who named a system. A component in exactly one system, which is nearly all
of them, never meets the distinction.

The seed stays **single-valued** whichever way it is chosen, and that is a
correctness requirement rather than a simplification: the rank below orders by band
and then depth with **no tiebreaker after that**, so two seeds in the same band
would resolve nondeterministically. Membership is many-valued; the chain it feeds
is not.

**Secrets carry no system band at all.** A credential authenticates a session with
the device itself, a shared component has one password, and the room it happens to
serve is the wrong owner for it. That is an ownership decision, not a tiebreak, and
it removes the case where an ambiguous inheritance would have been actively
dangerous.

## The resolve view

For a target entity and a key, return:

- the **effective value**;
- the **winning source** (a tree node, or a group + its weight);
- the **ordered shadowed bindings** it beat (source + value).

For rule-sets: the accumulated set, each rule tagged with its source and any
**suppressions**. One view explains both override (variables / tags) and accumulation
(rules).

## Worked example

RM204 codec (Room Kit Pro, fw 11.2) at Room RM204 -> Floor 3 -> HQ Building ->
HQ Campus, in the Huddle Room AV system. Member of two groups: **Old-firmware
Room Kits** (weight 450) and **PCI-scope** (weight 250). Specificity bands are
illustrative: `0` global, `100/200` templates, `300s` location by depth, `400s`
system by depth, `500` the instance. These single numbers are a presentation
flattening of the segmented key `(segment_rank, depth, group_weight, creation_order)`,
not the comparison itself.

```text
RM204 - cascade precedence            most-specific (highest) wins
===================================================================
 spec  source                              poll_interval   credential
 ----  ----------------------------------  -------------   ----------
 500   component RM204  (explicit)          -               -          <- ceiling
 450   group: Old-firmware Room Kits        5min  *         -
 440   system: Huddle Room AV system        -               -
 340   location: Room RM204                 -               -
 330   location: Floor 3                    -               vault-B  *
 320   location: HQ Building                -               -
 310   location: HQ Campus                  -               vault-A
 250   group: PCI-scope                     -               vault-C
 200   system_template: Std Huddle Room     -               -
 100   component_template: Room Kit Pro     30s             -
   0   global                               60s             -
===================================================================
 effective:  poll_interval = 5min    (group 450; shadowed template 30s, global 60s)
             credential    = vault-B (location Floor 3 @330; shadowed PCI-scope @250, Campus @310)
```

The two columns are the point of the shared scale:

- **`poll_interval`**: the **Old-firmware group (450)** sits *above* deployment, so
  its `5min` workaround beats the template / global defaults, what a fleet-wide bug
  fix needs.
- **`credential`**: the **PCI-scope group (250)** sits *below* deployment, so the
  specific **Floor 3 (330)** setting beats it, the case a fixed band would get
  wrong.

`component RM204 (500)` would top everything if it set a value directly. Additive
rules accumulate down this same ladder, and a group can **suppress** one by name
(the Old-firmware group suppresses the false-firing `high_memory` alarm).

## Resolution, in one line

Build the entity's ordered layer path, place matching groups on it by weight, fold
variables (override) / tags (union + override) / rules (additive + suppress) down the
combined specificity order, and emit effective values with provenance.
