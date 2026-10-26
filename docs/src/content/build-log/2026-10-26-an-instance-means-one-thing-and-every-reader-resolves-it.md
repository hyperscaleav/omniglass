---
title: "An instance means one thing, and reconciliation and settlement resolve it (#456)"
date: 2026-10-26
refs: [456, 430, 1020]
---

The ingest stamped every node-lane sample with the endpoint that collected it, while reconciliation
and settlement read only the owner-level series, so a property collected through an endpoint never
appeared as `is` and an un-instanced command no node had carried could only time out.
[ADR-0456](/architecture/decisions/#adr-0456-an-instance-names-which-value-of-one-key-on-one-owner-a-row-is-and-an-owner-level-read-reduces-across-every-instance)
gives the column one meaning (which of several values of one key on one owner a row is), names its
stamper per provenance (the observer for observed, owner-level for declared, calculated and health,
the command's for intended), and one rule for every reader: a question about the owner reduces
across every instance of the key, and a question naming an instance never does.

The reads and the two reductions are one primitive (`internal/storage/instance_values.go`): every
instance's current value of a key on an owner, newest first, on either sample lane, and two pure
reducers over it. Reconciliation reduces worst-case: drift when any observed instance disagrees with
want, `is` the newest disagreeing value (else the newest agreeing one), and the read now names the
instance it came from (`is_instance` on `GET /components/{name}/reconciliation`). `told` is the
newest intended value on any instance. Settlement reduces any-instance-agrees for a command with no
instance that no endpoint has carried: an observation on any instance runs its settle check and the
first agreeing one settles it, while a command dispatched over an endpoint stays judged on that
endpoint's series alone (#900). The verdict a command's issue answers with now reads the same
observation the recorded outcome did, where it used to read the owner-level series even for a
dispatched command.

Proven test-first: the pure reducers' tables, reconciliation over one and two endpoints against real
Postgres (agreeing, disagreeing, worst-case across two, told from an instanced command), the
undispatched command settling on any agreeing instance and failing when none agrees, a dispatched
one ignoring another instance, the API read naming the endpoint, and end to end over the real bus
(`internal/bus/instance_resolution_test.go`): a node's reading with no instance lands under its
endpoint, and reconciliation and the command's settlement both resolve that same row. The vitals
row's display rule (the newest across instances, named) is recorded for #1020 to build.
