---
title: "One identity cell replaces four column idioms"
date: 2026-08-04
refs: [553, 554]
form: bullet
---

- **One identity cell replaces four column idioms** (#553, #554). A list rendered an entity's identity
  four different ways depending on the page: separate `Name` and `Display name` columns, `Key` and
  `Label` columns, one `Name` column with an inline muted segment, or the segment alone. Sixteen
  hand-written column definitions, and the header word for the same fact differed between them.
  `IdentityCell` and `identityColumn` state the rule once (label on top, segment beneath, suppressed
  when equal, no uuid in a list), matching what `TreeList` already rendered. The keyspace pages keep
  `Key` as their header word and deliberately do not derive, since `icmp.rtt_avg` is a legal key and
  an illegal segment. On the write side, `createIdentity` had three consumers out of twenty surfaces
  despite being built for exactly this; every segment-bearing create form now consumes it. The
  recon that preceded the work corrected the plan: the rule was going to move into `ListShell` and
  `DetailShell`, but `ListShell` is chrome only, `DetailShell` contains no shell (it exports
  `RelatedList` and the filename is a leftover), and `Page` has one consumer in the SPA, so the rule
  had no home to move into and needed a primitive built instead.
