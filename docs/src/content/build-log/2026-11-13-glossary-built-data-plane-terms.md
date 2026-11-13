---
title: "Glossary: built data-plane terms move to the built table"
date: 2026-11-13
refs: [1126, 430, 1385]
---

- **Glossary: built data-plane terms move to the built table.** Twenty-six built terms moved
  from the `:::design[Data-plane terms, tracked in #430]` fence to the built table at the top of
  the glossary. Sixteen were built in #816, #817, #1049, #1056, #1035, #1037, #821 and #1078
  (session, auth step, connection fingerprint, reachability ladder, endpoint rejection, host key
  pin, certificate pin, tls mode, community, listen port, retries, pinned key / offered key, login
  dialog, terminal error, listener, reply shape). Identity binding (#1385, ADR-1282) moved with
  them. Nine more the fence still held are built too: on-demand node run (ADR-1163), message,
  server-sent events stream and field match (#1038), correlation and upgrade (#1034), conduit
  component, unbound identity and fan-out (ADR-1282). The fence now holds only terms that are not
  built: session_log, internal_log, raw_sample, collection.failed, discovery_rule, source registry,
  unit registry, fusion and correlation_id / source_event_id, each row naming its open tracker
  (#430, #137, #1251, #1395). The `## Design-only terms` intro now says a term is design in whole
  or in part, since a neighboring column or row of a few of them is live, rather than that nothing
  is implemented.
