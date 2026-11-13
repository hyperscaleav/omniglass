---
title: "Glossary: built data-plane terms move to the built table"
date: 2026-11-13
refs: [1126, 430, 1385]
---

- **Glossary: built data-plane terms move to the built table.** Seventeen data-plane terms
  built in #816, #817, #1049, #1056, #1035, #1037, #821 and #1078 (session, auth step,
  connection fingerprint, reachability ladder, endpoint rejection, host key pin, certificate pin,
  tls mode, community, listen port, retries, pinned key / offered key, login dialog, terminal error,
  listener, reply shape) moved from the `:::design[Data-plane terms, tracked in #430]` fence to
  the built table at the top of the glossary. Identity binding, built by #1385 and ADR-1282, also
  moved to the built table. The design fence now contains only terms whose tracking issue remains
  open: on-demand node run, message, server-sent events stream, field match, correlation, upgrade,
  session_log, internal_log, raw_sample, collection.failed, conduit component, unbound identity,
  fan-out, discovery_rule, source registry, unit registry, fusion, and correlation_id / source_event_id.
  The glossary heading "## Design-only terms" now accurately states that every term below is unbuilt,
  closing #1126.
