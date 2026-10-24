---
title: "A series tiebreak is checked against the database rather than against a reading of the code"
date: 2026-08-13
refs: []
form: bullet
---

- **A series tiebreak is checked against the database rather than against a reading of the code.** A
  single observed flake was filed as the series resolver breaking a `ts` tie on a random uuid, with
  the mechanism read off the resolver rather than off the failure. The premise does not hold, and the
  tiebreak the issue asked for is the one already there: `property.id` is a bigint identity column,
  so `order by ts desc, id desc` breaks a tie on insertion order, and the test the flake came from
  cannot tie at all, since every declared write is its own transaction and `ts` defaults to
  `transaction_timestamp`. No fix shipped for a mechanism that does not exist. What shipped is the
  proof: a test that reads the column's shape out of the live catalog, so converting it to a random
  uuid fails loudly with the reasoning named, and that drives a genuine tie the public API cannot
  produce, resolving it to the later insert twenty times over. The ordering's one real dependency is
  now stated where it is relied on: `ts` leads because the observed lane accepts a caller-supplied
  timestamp, so a clock stepping backwards between two writes to one series resolves to the
  earlier-written row, which no tiebreak can reach. Sixty-five repeats of the original test, forty of
  them under concurrent package load, did not reproduce the failure.
