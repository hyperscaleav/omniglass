---
title: Cascade
description: How effective settings (config, variables, tags, rule-sets) are resolved for any entity and how the resolve view explains why a given value won.
sidebar:
  badge:
    text: Partial
    variant: note
---

:::note[Partial]
The **binding chain** is built for the three cells that ride it: a
[tag](/architecture/tags/), a [variable](/architecture/variables/), and a secret each own a value on the
exclusive arc `platform | location | system | component` and resolve most-specific-wins down the
location, system, and component trees (union-on-key for tags), with the least-specific tier named
**`platform`** and gated by its own `platform:<action>` permission
([ADR-0057](/architecture/decisions/#adr-0057-the-cascades-least-specific-tier-is-platform-and-a-default-is-not-a-tier)).
The same primitive resolves down the principal axis for [settings](/architecture/settings/). Still
`Design`: the two **template** bands, [group](/architecture/groups/) placement by weight, additive
**rule** accumulation with suppression, and the operator-facing **resolve view** (winner plus ordered
shadowed bindings, including the fall-through-to-declaration provenance).
:::

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
  chain: deployment beats template bindings; deepest location wins)*
- **Composition tightens the part.** My "Standard Huddle Room" template polls codecs
  every 30s, tighter than the 60s the codec's own template binds, and every codec
  placed in a huddle room picks that up. *(system_template beats component_template)*
- **"Why did it get this?"** RM204 polls every 5 minutes and I don't know why; the
  resolve view shows it is the "Old-firmware Room Kits" group (weight 450) shadowing
  the template's 30s. *(the effective-config resolve view)*

The cross-cutting cases (a fleet-wide fix that auto-clears, a broad policy as a floor, a hand-picked
set) are the [group](/architecture/groups/) stories, where the group is placed by weight on this same
scale.

## The structural chain

Broad decision to specific deployment; most-specific (deepest) wins:

```text
platform              the install-wide binding
component_template    the leaf entity's template bindings
system_template       the leaf's owning-system template bindings
location tree         earth -> luna -> port-lovell             (deepest wins)
system tree           parent system -> subsystem -> ...        (deepest wins)
component tree        chassis -> card -> ...                   (deepest wins, = the leaf)
```

- Resolution runs **for the leaf** over its containment path; a non-leaf entity
  (a chassis, a floor) resolves over its own shorter subset.
- **`platform` is the least-specific rung, not a floor under the chain.** It is what
  an admin set for the **whole install**, one decision like every other rung, and a
  write there needs the install-wide `platform:<action>` permission on top of the
  resource's own ([identity and access](/architecture/identity-access/#install-wide-authority-is-not-estate-scope)).
- **There is no root location.** The location tree is a forest with N unparented
  tops, and a top is an ordinary location: binding at `earth` misses `mars`, and a
  top added next quarter is silently uncovered. A tier above today's tops is a new
  `location_type` and a real node, never a magic one, which is why "everything" is
  `platform` and not a location at all.
- **Philosophy: deployment beats the template.** HQ's location credential overrides
  what the Room Kit template binds; "Standard Huddle Room" overrides what the bare
  Room Kit binds.
- **Templates are the leaf's base.** Ancestor nodes in the trees contribute their
  **instance** bindings, not their templates (a chassis hands a card its
  chassis-wide credential; the card keeps its own template).
- The three structural segments are **variable-depth trees** (parent-reference
  nesting, arbitrary depth); the deepest node wins. Weight-free, pure depth.

## Bindings cascade; declarations do not

There are two structures here, and only one of them is the cascade.

**The binding side is the cascade.** Every rung above is something somebody
**decided**: a template author's value, an operator's at a location, an admin's for
the whole install. They order by deployment specificity and fold most-specific-wins.

**The declaration side is what a thing *is*** absent any decision. A **default**
lives there, as a column on a definition row, beside the unit, the kind, and the
validation rule. It is not a rung: it shadows nothing and nothing shadows it. The
cascade **falls through** to it when no rung bound anything, and the resolve view
reports that as a declaration rather than as a winning source, because "an admin
chose this for everything" and "nobody chose, so the type's declaration stands" are
different facts an operator needs to tell apart.

A default is a column on a definition row, so **a kind with no definition table has
no default**. Absent means absent:

| Kind | Where its default is declared |
|---|---|
| [setting](/architecture/settings/) | The tagged struct field on `Settings` (its `default:` tag). |
| [property](/architecture/variables/#property-one-typed-name-a-classifier-contract-a-stored-value) | The **classifier contract's** `default_value` column (`product_property`, `standard_property`, `location_type_property`), the shipped instance of the pattern: `EffectiveProperties` reads `coalesce(the instance's set value, the contract default)` ([ADR-0047](/architecture/decisions/#adr-0047-the-fields-fold-product_property-and-property_value)). |
| [variable](/architecture/variables/) | None. |
| secret | None. |
| [tag](/architecture/tags/) | None. |

Note where a property's default is **not**: the `property` catalog entry carries the
`data_type`, the unit, and the validation rule, but no value. A default is what a
thing is under a given contract, and the contract is the classifier's, so the column
lives on `product_property` and its two siblings. That is also the second ordering
this side allows: a default may narrow along **type** specificity, one product or
standard declaring a different default for the same catalog property. Narrowing on
the declaration side never mixes with the binding cascade. A catalog-wide default
beneath the contract ones does not exist today; if it lands, it lands as another
column on the declaration side, not as a rung.

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
operator never tunes it: `platform` lowest, then the templates, then the location / system / component
trees by depth, then the entity's own **instance** at the ceiling. A **group's weight is its
specificity on that same scale**, so a group sits wherever its weight lands relative to the structural
bands: a high weight beats deployment (a must-apply override), a low weight loses to it (a baseline
deployment overrides). The instance ceiling beats any group; equal specificity breaks by creation
order. A typed group applies at its own level: a component-group to components directly; a system-group
reaches a component **through the system layer** of its cascade.

So however many groups an entity is in, the group band collapses to one weighted list on the shared
scale, fully predictable, and the resolve view names the winner.

**The comparison key is segmented, not a single number.** Precedence is a lexicographic key
`(segment_rank, depth, group_weight, creation_order)`, compared field by field in that order. The
`segment_rank` orders the structural bands (platform, templates, location / system / component trees, the
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

`datapoint_type` defines **identity** (kind, unit, validation, fusion_policy) for
every datapoint key, which the cascade never overrides (policy, not ontology). A
type's **default** lives on that declaration too, off the cascade: it is what the
value is when no layer bound anything, not a rung the layers compete with. The
resolve view reports it as a declaration rather than as a winning source.

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

When **no rung bound the key**, there is no winning source and nothing was shadowed:
the view reports the **declaration** ("no binding; the type declares 60s") as its own
provenance, never as a bottom entry in the shadowed list. Telling "an admin set this
install-wide" from "nobody set it, so this is what the thing is" is the whole point of
keeping the two structures apart, and this view is where an operator learns it.

For rule-sets: the accumulated set, each rule tagged with its source and any
**suppressions**. One view explains both override (variables / tags) and accumulation
(rules).

## Worked example

RM204 codec (Room Kit Pro, fw 11.2) at Room RM204 -> Floor 3 -> HQ Building ->
HQ Campus, in the Huddle Room AV system. The estate has three unparented tops (HQ
Campus, East Campus, Airport Office), so HQ Campus is a **top, not a root**: it has
no parent and no siblings' subtrees. Member of two groups: **Old-firmware Room Kits**
(weight 450) and **PCI-scope** (weight 250). Specificity bands are illustrative: `0`
platform, `100/200` templates, `300s` location by depth, `400s` system by depth, `500`
the instance. These single numbers are a presentation flattening of the segmented key
`(segment_rank, depth, group_weight, creation_order)`, not the comparison itself.

```text
RM204 - cascade precedence            most-specific (highest) wins
==============================================================================
 spec  source                            poll_interval  credential   retry_limit
 ----  --------------------------------  -------------  ----------   -----------
 500   component RM204  (explicit)        -              -            -      <- ceiling
 450   group: Old-firmware Room Kits      5min  *        -            -
 440   system: Huddle Room AV system      -              -            -
 340   location: Room RM204               -              -            -
 330   location: Floor 3                  -              vault-B  *   -
 320   location: HQ Building              -              -            -
 310   location: HQ Campus  (a top)       -              vault-A      -
 250   group: PCI-scope                   -              vault-C      -
 200   system_template: Std Huddle Room   -              -            -
 100   component_template: Room Kit Pro   30s            -            -
   0   platform  (admin, install-wide)    60s            -            -
==============================================================================
 effective:  poll_interval = 5min    (group 450; shadowed template 30s, platform 60s)
             credential    = vault-B (location Floor 3 @330; shadowed PCI-scope @250, HQ Campus @310)
             retry_limit   = 3       (no binding at any rung: the declaration stands)
```

The three columns are the point:

- **`poll_interval`**: the **Old-firmware group (450)** sits *above* deployment, so
  its `5min` workaround beats the template's `30s` and the admin's install-wide
  `60s`, what a fleet-wide bug fix needs.
- **`credential`**: the **PCI-scope group (250)** sits *below* deployment, so the
  specific **Floor 3 (330)** setting beats it, the case a fixed band would get
  wrong.
- **`retry_limit`**: **nothing bound it anywhere**, so the fold ends empty and the
  value falls through to the declaration. `3` is not a `platform` binding an admin
  made and it did not shadow anything; it is what the type says a retry limit is.

Binding `credential` at **HQ Campus** covers HQ and nothing else: East Campus and
Airport Office are separate tops, and a fourth site added next year starts uncovered.
The install-wide `60s` at **`platform`** is the only rung that reaches all of them,
which is why the tier exists rather than a synthetic root location.

`make dev` seeds this estate, so the rule is inspectable rather than illustrative:
three unparented tops with a device under two of them, one carrying the `staging`
tag its subtree's binding sets and the other the `prod` the `platform` binding sets.

`component RM204 (500)` would top everything if it set a value directly. Additive
rules accumulate down this same ladder, and a group can **suppress** one by name
(the Old-firmware group suppresses the false-firing `high_memory` alarm).

## Resolution, in one line

Build the entity's ordered layer path, place matching groups on it by weight, fold
variables (override) / tags (union + override) / rules (additive + suppress) down the
combined specificity order, fall through to the type's declaration where the fold is
empty, and emit effective values with provenance.
