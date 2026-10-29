---
title: "An operator's row shadows an official row of the same name in another registry (#1307)"
date: 2026-10-29
refs: [1307, 1265, 318]
---

A release that seeds a name an operator already holds in another ingest registry (their custom
property `x`, then a shipped metric `x`) left the name in two catalogs, and every sample carrying it
was refused. Now the boot seed still upserts the official row, which is always the platform's, and
on that install the operator's row shadows it: their series keep landing where they did
([ADR-1307](/architecture/decisions/#adr-1307-an-operators-row-shadows-an-official-row-of-the-same-name-in-another-registry)).

The rule is one function, `storage.ResolveNameLane`, and every reader of a bare name calls it: the
ingest registry, an event rule's sample key, a calc rule's key and the catalog a driver spec is
validated against. `TestEveryNameResolverAgreesWithTheShadow` holds all four to the same answer. A
reserved key held twice is never shadowed. The registry cache reports a shadowed name on a rebuild
and hourly, apart from collisions, and a push of the shadowed official row is refused with a
reason that says it is shadowed. Deleting the operator's row lets the official one resolve.

The same name in the same registry still cannot coexist: the seed takes the operator's row over.
#318 answers that with namespaced keys, where a bare name resolves the operator's namespace before the platform's.
