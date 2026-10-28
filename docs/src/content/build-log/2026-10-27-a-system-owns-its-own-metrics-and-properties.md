---
title: "A system owns its own metrics and properties (#422)"
date: 2026-10-27
refs: [422, 1282, 1297]
---

A system now owns telemetry of its own on both producers that have one. A system calc rule's output
was already a system-owned sample (#997); a test now drives it end to end through the real server and
the CLI and reads it back on the system's metrics and properties. The new producer is the push lane:
`POST /telemetry:push` takes `owner.kind: system`, and the system's metric, property and event land
on its own arc, observed, at the owner level, with the batch's source. The ingest stamps the arc the
binding resolved on every lane instead of a hardcoded component.

The fence is ADR-0752's, joined by the system tier as that decision said it would be: `telemetry`
resolves on the component and system tiers, the push scope alone sees a system it may push to, an
unreadable miss is a 404 and a readable one a 403, once per batch, and a test pins that the two tiers
do not reach each other. A system has no endpoints and owns no log lines, so a system push naming an
instance or carrying a log line is a 422 before anything is published. A system's observed property
reads on its change history (`omniglass system property sample list`) until the unified properties
read (#1297) carries observed values beside declared ones for components and systems alike.

The ruling narrowed the issue (ADR-0422): location-owned observed telemetry is out, and the docs stop
claiming it; a location keeps its calculated health. Node-collected system telemetry is the
shared-API fan-out filed as #1282, extending the webhook ingress, rather than a system-owned endpoint.
