---
title: "The simulator catalog drops its fleet and room archetypes (#914)"
date: 2026-11-12
refs: [914, 911, 1553, 1266]
---

The embedded simulator catalog had two layers above the devices that spoke a vocabulary of their
own: room archetypes (`systems/*.yaml`, with roles such as `codec` and `touch-controller`) and fleet
manifests (`fleets/*.yaml`, with system types such as "Standard Conference Room"), compiled by an
internal fleet compiler. Since #911 a simulated system is built to a standard, and since #1553 a
scale lab scenario is the standards-plus-counts format, so both layers are gone, with the compiler,
`profile.SystemProfile` and `Store.Systems`. What the engine and the scenario resolver still use,
`fleet.Enrich` and `fleet.NameGenerator`, stays.

A test over the catalog holds the line: it fails while a `systems/` or `fleets/` directory is
embedded, or any file names a role or system type the template catalog's standards and the registered
system types do not. The acceptance test serves one system of each standard the `corporate`
scenario places, resolved by the scale lab's own resolver, and the 1,000-device benchmark runs
unchanged. The decision is [ADR-0914](/architecture/decisions/#adr-0914-the-simulator-catalog-carries-no-fleet-or-room-archetype-vocabulary).
