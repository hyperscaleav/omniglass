---
title: "The stateless arc's review hardening"
date: 2026-10-17
refs: []
form: bullet
---

- **The stateless arc's review hardening.** A `/code-review` pass over the whole arc before
  merge closed four robustness gaps, each with a regression test. The worklist no longer masks
  an infrastructure fault as a missing credential: a real unseal failure (a key provider that
  cannot open the envelope, a DB error) now fails the pull so the node keeps its last-good
  worklist and retries, while only genuine absence (not-found or an ambiguous name) still rides
  as a `collection-failed`. Driver-spec validation gained the symmetric transport-shape check it
  was missing: a `tcp` poll emit that locates by oid, key, or jsonpath (forms a line response
  cannot feed) refuses at write, the way an `snmp` emit already had to locate by oid, and the
  per-emit extractor check runs first so a malformed extractor names its own fault. The SNMP
  interpreter strips a leading dot from an emit's OID the same way the getter strips it from the
  response, so a spec authored in the standard MIB `.1.3...` form matches instead of faulting
  every tick. And the node prunes its per-command idempotence memory past the redelivery window,
  so a long-lived node's command map no longer grows for the life of the run (#603).
