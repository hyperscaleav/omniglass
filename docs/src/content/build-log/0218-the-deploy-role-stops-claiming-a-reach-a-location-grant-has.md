---
title: "The deploy role stops claiming a reach a location grant has never had"
date: 2026-08-13
refs: []
form: bullet
---

- **The deploy role stops claiming a reach a location grant has never had.** Its operator-facing
  description, rendered in the Roles view and in the grant builder's tooltips, said that granted at a
  location it "builds out and edits everything inside a subtree (add rooms, systems, and
  components)". It does one of those three. A grant contributes to a resolved scope only when its
  scope kind can CONTAIN the resource, and each tree tier is contained by its own kind alone, so a
  location-kind grant fills no system-tier and no component-tier scope whatever actions the role
  carries: the shipped `tech-east` principal cannot create a system in its own building, and cannot
  read a component in it either. The gap was silent and expensive in exactly the way a stale sentence
  is: an admin granted the role expecting one thing, the console offered the surfaces, and the server
  refused. The description now says what the grant does, and two tests hold it there: a pure one over
  the embedded `roles.yaml` that computes the whole reach matrix through the same resolver the
  gateway uses (so a cross-tier rule landing shows up as a failing expectation rather than as
  prose going stale), and an end-to-end one that drives the component tier the way the system tier
  was already driven. The capability itself, a scope that spans tiers, is
  [#10](https://github.com/hyperscaleav/omniglass/issues/10) and unbuilt; this slice is the claim.
