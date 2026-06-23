---
title: Groups
description: "Named sets of component, system, location, or principal: static or dynamic membership, weighted, a cascade overlay and an access scope."
sidebar:
  badge:
    text: Spec
    variant: caution
---

Leaf of the [architecture spine](/architecture/). A **group** is a named set of entities that cuts
across the structural trees. The structural tree handles config by position and kind; groups handle
config by attribute or by a hand-picked set. One "set of entities" primitive serves two jobs: a
[cascade](/architecture/cascade/) overlay and an [access](/architecture/identity-access/) scope, which
an anonymous predicate never could.

## What a group is

A group:

- is **component / system / location / principal** kind (matching the structural levels, plus
  principals for access);
- has **static** membership (an explicit list) or **dynamic** membership (a filter, re-evaluated live
  as attributes change, so a device leaves the moment it stops matching);
- has a **weight** (its specificity on the shared scale, see *Placement*; the only weights in the
  system);
- carries **variable / tag / rule** bindings, with the same per-kind combinators the cascade uses;
- is also the unit of **access control**: a visibility / permission scope (see
  [identity and access](/architecture/identity-access/)).

| Table | Key columns | Notes |
|---|---|---|
| `group` | id, kind (component/system/location/user), membership (static list or dynamic filter), **weight** | cascade band and access scope ([cascade](/architecture/cascade/), [identity and access](/architecture/identity-access/)) |

## Placement: one specificity scale

Structural layers auto-derive a specificity from position, weight-free, and the operator never tunes
it: `global` lowest, then the templates, then the location / system / component trees by depth, then
the entity's own **instance** at the ceiling. A **group's weight is its specificity on that same
scale**, so a group sits wherever its weight lands relative to the structural bands: a high weight
beats deployment (a must-apply override), a low weight loses to it (a default that deployment
overrides). The instance ceiling beats any group; equal specificity breaks by creation order. A typed
group applies at its own level: a component-group to components directly; a system-group reaches a
component **through the system layer** of its cascade.

## Multiple membership

An entity belongs to a flat **set** of groups. Collect all their bindings and fold by specificity
(weight): highest wins for variables and tag-values; rules accumulate, with weight resolving any
add-vs-suppress conflict; equal weights break by creation order. There is no second precedence axis.
So however many groups an entity is in, the group band collapses to one weighted list on the shared
scale, fully predictable, and the [resolve view](/architecture/cascade/) names the winner.

## No nesting

Groups do not contain groups. A dynamic filter already expresses a union (`type in (codec, display)`)
and multiple membership covers the rest, so nesting would earn only a narrow "DRY union of static
sets" case, not worth its transitive-membership and cycle-guard cost. Add it later if that case ever
bites.

## Types are not layers

A `_type` (device/app, AV-System, room) is a classification attribute, resolved by a **group** filter
(`type == X`), never a tree position. The tree is structural; attributes are groups. This is the bridge
from the structural [cascade](/architecture/cascade/) to type-based policy: instead of a type layer in
the tree, you author a dynamic group filtered on type and place it by weight.

## What groups are for

In operator terms, the same user stories the cascade serves, the group-specific ones:

- **A fleet-wide fix that auto-clears.** Cisco firmware 11.2 has a memory leak, so I make a dynamic
  group `model == "Room Kit Pro" && firmware < "11.5"` at high weight, slow its poll and suppress the
  false high-memory alarm; the 23 affected codecs across 6 floors get it at once, and each drops out
  the moment it upgrades. *(dynamic group above deployment, live membership, rule suppression)*
- **A broad policy as a floor, not a ceiling.** I put baseline stricter thresholds on a low-weight
  "PCI-scope" group, so a lab on Floor 3 that needs its own values still wins; the policy is a default
  the specific deployment overrides. *(low-weight group below deployment, the shared specificity scale)*
- **A hand-picked set no filter can name.** I drop the 5 executive briefing rooms into a static "Exec
  Rooms" group for premium escalation, and grant the exec-support team visibility to that same group.
  *(static membership; groups double as the access scope)*
