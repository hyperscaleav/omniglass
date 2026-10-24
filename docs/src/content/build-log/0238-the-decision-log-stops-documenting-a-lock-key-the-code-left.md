---
title: "The decision log stops documenting a lock key the code left behind"
date: 2026-08-14
refs: []
form: bullet
---

- **The decision log stops documenting a lock key the code left behind**
  ([#717](https://github.com/hyperscaleav/omniglass/issues/717)).
  [ADR-0056](/architecture/decisions/#adr-0056-every-foreign-key-stores-a-primary-key) carved health
  out of the uuid conversion and stated why: its advisory lock hashed `health/<kind>/<name>`, so a
  mixed currency would hash two keys for one owner and silently stop serializing. The lock has hashed
  the row's **id** since the identity epic ([#627](https://github.com/hyperscaleav/omniglass/issues/627))
  landed its addressing slice ([#647](https://github.com/hyperscaleav/omniglass/issues/647)).

  This is more than a stale sentence, and the connection is the point: a name-keyed lock partitions
  an estate only while names partition it, and that same epic scoped a location's name uniqueness to
  its **placement**, so two rooms under different buildings may both be `415a`. The keying scheme
  stopped being safe at exactly the moment the epic made names non-unique. The code moved and the log
  did not, so the log was documenting a scheme that would be a live defect if anyone implemented it
  from the page.

  The entry is amended in place rather than renumbered, the original bullet left standing with a
  forward pointer, since the log is append-only and its argument (one currency per lock key) is the
  one that reversed its conclusion. The [health](/architecture/health/) page needed no change: it has
  said "owners are locked by id, in one order" since
  [#670](https://github.com/hyperscaleav/omniglass/issues/670) moved the lock ORDER for the same
  reason. The build log's own copy of the old claim is left exactly as it shipped, which is what this
  page is for.

  The fact now has a test rather than only a paragraph. The key moved into one named function
  (`healthLockKey`) and a unit test asserts what the amendment claims: two same-named rooms do not
  share a lock, one owner keys consistently, and the owner kind is part of the key.
