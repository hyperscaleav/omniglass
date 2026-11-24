---
title: Cascade
description: "How a value set once, high in the fleet, resolves onto every entity below it, and how the resolve view explains which binding won."
sidebar:
  order: 6
  badge:
    text: Partial
    variant: note
---

The **cascade** lets an operator set a value once, high up, have it apply to everything below, shadow
it on any one entity, and then see exactly why a given value won. It resolves
[variables, secrets](/architecture/variables/) and [tags](/architecture/tags/) today; the same
primitive resolves [settings](/architecture/settings/) along the user axis instead of the fleet.

## The structural chain

A value is a **binding**: a cell owned at one rung of the fleet on the exclusive arc
([core entities](/architecture/core-entities/#ownership-the-exclusive-arc)). Resolution runs for a
component over its containment path, least specific first, and the most specific binding wins:

```text
band 0  platform         the install-wide binding
band 1  location tree    campus -> building -> floor -> room      (deepest wins)
band 2  system tree      hall -> room subsystem                   (deepest wins)
band 3  component tree   chassis -> card                          (deepest wins, the leaf)
```

- **`platform` is a rung, not a floor.** It is an admin's decision for the whole install, written
  only with the install-wide `platform:<action>` permission on top of the resource's own
  ([ADR-0057](/architecture/decisions/#adr-0057-the-cascades-least-specific-tier-is-platform-and-a-default-is-not-a-tier)).
- **There is no root location.** The location tree is a forest of tops, so a binding at one campus
  misses the next, and a top added next quarter starts uncovered. "Everything" is `platform`.
- **Within a band, depth decides**: the nearest binding up the tree wins, weight-free.
- **Secrets have no system band.** A credential authenticates a session with the device itself, and
  the room a shared device serves is the wrong owner for it.

The precedence is a lexicographic key, `(band, depth)` today, compared field by field, so however
deep a tree runs it never overruns into another band.

## Structural multi-membership (a component in N systems)

A component can be a member of several systems
([membership](/architecture/core-entities/#membership-what-a-slot-attaches-to)), but the chain it
resolves over is single-valued: two seeds in one band would leave the fold with no tiebreak. So the
resolution **takes the system to resolve against**. Given one, it resolves through that system, and
only if the component is a member. Given none, it seeds from the component's **primary** membership,
which is all `is_primary` is for. `GET /components/{name}/effective-tags?system=<ref>` takes a system
today; the variable and secret reads resolve against the primary.

## Combinators

What is resolved decides how bindings combine:

- **Variables and secrets: scalar shadow.** One value per name; the most specific binding wins.
- **Tags: union on key, shadow on value.** Keys accumulate down the chain; for each key the most
  specific binding's value wins ([tags](/architecture/tags/)).

:::design[Rules on the cascade, additive with suppression, tracked in #10]
Event and calc rules join the cascade with a third combinator, **additive accumulation with explicit
suppression**: a component is governed by the union of rules bound at every rung, and a rung removes
one by name with a suppression. A [group](/architecture/groups/) is how most such bindings would be
placed.
:::

## Bindings cascade; declarations do not

Two structures sit side by side, and only one is the cascade. The **binding side** is every rung
above: something somebody decided, ordered by how specific the deployment is. The **declaration
side** is what a thing *is* absent any decision: a **default**, a column on a definition row. A
default is not a rung. It shadows nothing and nothing shadows it; the cascade falls through to it
when no rung bound anything. "An admin chose this for everything" and "nobody chose, so the type's
declaration stands" are different facts.

A default lives on a definition row, so a kind with no definition table has none:

| Kind | Where its default is declared |
|---|---|
| [setting](/architecture/settings/) | The `default:` tag on its field of the `Settings` struct |
| [property](/architecture/core-entities/#declared-properties-the-classifier-contracts-and-the-declared-rows) | The classifier contract's `default_value` (`product_property` and its siblings), read by `EffectiveProperties` |
| [variable](/architecture/variables/), secret, [tag](/architecture/tags/) | None |

A property type carries a name, data type and validation, but no value: a default is what a thing
is under a contract, and the contract is the classifier's.

## The resolve view

For a component and a key, the resolve view returns the effective value, the winning binding (its
owner, band and depth), and the ordered bindings it shadowed. Each of
`GET /components/{name}/effective-variables`, `.../effective-secrets` and `.../effective-tags`
returns that list, every entry marked `winner` or shadowed, and the console draws it as the
ResolutionPanel on the component's detail. A secret's entries are masked like every other secret
read. To use it, see [variables](/admin/variables/) and [secrets](/admin/secrets/).

## A worked example

Codec RM204 sits in Room RM204, on Floor 3 of the HQ Building on the HQ Campus, and is a member of
the Huddle Room system.

```text
RM204: precedence, most specific wins
===================================================================
 band  depth  binding owner                 poll-interval  credential
 ----  -----  ----------------------------  -------------  ----------
 3     0      component RM204               -              -
 2     0      system Huddle Room            30s            -
 1     0      location Room RM204           -              -
 1     1      location Floor 3              -              vault-b
 1     3      location HQ Campus (a top)    -              vault-a
 0     -      platform                      60s            vault-z
===================================================================
 effective: poll-interval = 30s     (system; shadowed platform 60s)
            credential    = vault-b (Floor 3; shadowed HQ Campus, platform)
```

The credential binding at HQ Campus covers HQ and nothing else; East Campus and the Service Depot
(a building at the top, with rooms straight off it) are covered only by the `platform` binding. The
dev seed a [lab](/developer/lab/) starts with carries four such tops, so the rule is inspectable.

:::design[Target design: the classifier bands, tracked in #1255 and #184]
Classifier bands join the chain above the trees: a product's binding applies to every component of
that product, a standard's to every conforming system, and the deployment still beats the classifier
("Standard Huddle Room" polls codecs every 30 s, tighter than the codec product's 60 s). Ancestor
entities contribute their instance bindings, never their classifiers. Property values resolve across
owners this way in [#1255](https://github.com/hyperscaleav/omniglass/issues/1255); secrets and
variables gain the bands in [#184](https://github.com/hyperscaleav/omniglass/issues/184).
:::

::::design[Target design: groups placed by weight, tracked in #10]

## Groups overlay the cascade, placed by weight

A [group](/architecture/groups/) cuts across the trees by attribute or hand-picked set, and its
weight is its specificity on the cascade's one scale. A high weight beats deployment (a fleet-wide
firmware workaround), a low one loses to it (a policy floor a lab may override). The key becomes
`(segment, depth, group_weight, creation_order)`, and the instance itself stays the ceiling. The
resolve view then names a group and its weight as a winning source, and reports a fall-through to a
declaration as its own provenance rather than as a shadowed entry.
::::
