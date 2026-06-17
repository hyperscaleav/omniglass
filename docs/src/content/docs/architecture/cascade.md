---
title: Cascade
description: How effective settings (config, variables, tags, rule-sets) are resolved for any entity and how the resolve view explains why a given value won.
---

Component document of
[architecture overview](/architecture/). How effective settings
(config, variables, tags, rule-sets) are resolved for any entity, and how to explain why a
given value won.

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
- **A fleet-wide fix that auto-clears.** Cisco firmware 11.2 has a memory leak, so I
  make a dynamic group `model == "Room Kit Pro" && firmware < "11.5"` at high weight,
  slow its poll and suppress the false high-memory alarm; the 23 affected codecs
  across 6 floors get it at once, and each drops out the moment it upgrades.
  *(dynamic group above deployment, live membership, rule suppression)*
- **A broad policy as a floor, not a ceiling.** I put baseline stricter thresholds
  on a low-weight "PCI-scope" group, so a lab on Floor 3 that needs its own values
  still wins; the policy is a default the specific deployment overrides. *(low-weight
  group below deployment, the shared specificity scale)*
- **A hand-picked set no filter can name.** I drop the 5 executive briefing rooms
  into a static "Exec Rooms" group for premium escalation, and grant the exec-support
  team visibility to that same group. *(static membership; groups double as the
  access scope)*
- **"Why did it get this?"** RM204 polls every 5 minutes and I don't know why; the
  resolve view shows it is the "Old-firmware Room Kits" group (weight 450) shadowing
  the template's 30s. *(the effective-config resolve view)*

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

## Types are not layers

A `_type` (device/app, AV-System, room) is a classification attribute, resolved by
a **group** filter (`type == X`), never a tree position. The tree is structural;
attributes are groups.

## Combinators (by what is resolved)

- **config / variables -> scalar override**: the deepest/highest source wins; one value.
- **tags -> union on name, override on value**: names accumulate; for a given
  name, the winning source's value wins.
- **rules** (`calc_rule` / `event_rule`) -> **additive
  accumulation + explicit suppression**: a leaf is governed by the union of rules
  from every layer; a layer removes one by name with a suppression.

## Groups (cross-cutting, placed by weight)

The structural tree handles config by position and kind. **Groups** handle config
by attribute or by a hand-picked set, cutting across the tree. A group:

- is **component / system / location** kind (matching the structural levels);
- has **static** membership (an explicit list) or **dynamic** membership (a
  filter, re-evaluated live as attributes change, so a device leaves the moment it
  stops matching);
- has a **weight** (its specificity on the shared scale, see *Placement*; the only weights in the system);
- carries **variable / tag / rule** bindings, with the same per-kind combinators;
- is also the unit of **access control**: a visibility / permission scope (see
  [identity-access](/architecture/identity-access/)). One "set of entities" primitive serves both config and
  authZ, which an anonymous predicate never could.

**Placement: one specificity scale.** Structural layers auto-derive a specificity
from position, weight-free, the operator never tunes it: `global` lowest, then the
templates, then the location / system / component trees by depth, then the entity's
own **instance** at the ceiling. A **group's weight is its specificity on that same
scale**, so a group sits wherever its weight lands relative to the structural
bands: a high weight beats deployment (a must-apply override), a low weight loses
to it (a default that deployment overrides). The instance ceiling beats any group;
equal specificity breaks by creation order. A typed group applies at its own level:
a component-group to components directly; a system-group reaches a component
**through the system layer** of its cascade.

**Multiple membership.** An entity belongs to a flat **set** of groups. Collect all
their bindings and fold by specificity (weight): highest wins for variables and
tag-values; rules accumulate, with weight resolving any add-vs-suppress conflict;
equal weights break by creation order. There is no second precedence axis.

**No nesting.** Groups do not contain groups. A dynamic filter already expresses a
union (`type in (codec, display)`) and multiple membership covers the rest, so
nesting would earn only a narrow "DRY union of static sets" case, not worth its
transitive-membership and cycle-guard cost. Add it later if that case ever bites.

So however many groups an entity is in, the group band collapses to one weighted
list on the shared scale, fully predictable, and the resolve view names the winner.

## The registry is outside the cascade

`datapoint_type` defines **identity** (kind, unit, validation, fusion_policy)
for every datapoint key, which the cascade never overrides (policy, not ontology).
Ship-with **default policy** lives at `global`, the floor of the chain.

## Structural multi-membership (a component in N systems)

Distinct from group membership: a shared device in N systems would make the system
layer a DAG. Keep it a tree with a **primary-system pointer** (which system chain
feeds the cascade); a truly shared device **skips the system layer**. The genuine
"config differs per system" case is answered by **per-system effective views** on
demand, not by merging chains into the resolution.

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
system by depth, `500` the instance.

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
