---
title: "The role and choice seed lane is retired (#1332)"
date: 2026-11-14
refs: [1332, 827, 1324]
---

With the standards boot seed gone (#1324), `SeedSystemRole` and `SeedRoleChoice` had no production caller. They are
deleted from the Storage Gateway, its interface and `UnimplementedGateway`, with the repair of a role an interrupted
seed left without its typed slot and the seed's deferral to a system's own role of the name. A standard's roles and
choices are written by `CreateStandardFromTemplate`, which is unchanged. The tests that used the seed as a fixture
now plant through it (choices, alternates, health verdicts, the alternate round trip over the API), the ADR-0503
collision rule is pinned on the standard's role write, and the tests that pinned only the seed lane (the seed repair
and the choice reconcile-on-reseed) are dropped. [ADR-0508](/architecture/decisions/#adr-0508-the-boot-seed-installs-a-shipped-role-whole-and-repairs-a-set-an-unrevised-role-is-missing)
is marked superseded by ADR-0827.
