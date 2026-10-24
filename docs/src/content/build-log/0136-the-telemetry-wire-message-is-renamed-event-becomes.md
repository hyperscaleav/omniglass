---
title: "The telemetry wire message is renamed: `Event` becomes `TelemetryBatch` ([#424](https://github.com/hyperscaleav/omniglass/issues/424))"
date: 2026-07-29
refs: [424]
form: bullet
---

- **The telemetry wire message is renamed: `Event` becomes `TelemetryBatch`
  ([#424](https://github.com/hyperscaleav/omniglass/issues/424)).** The protobuf message a node ships was
  called `Event`, but an event in this platform is a semantic occurrence (`event_type`-registered, carrying
  an `origin`, per [ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events)),
  while the message is the **envelope** that carries samples and, since the log lane landed, raw log lines.
  Naming the envelope after one of its passengers had put four different meanings on the word "event",
  three of them in a single function signature. So `proto/og/v1/event.proto` becomes `telemetry.proto`,
  the message becomes **`TelemetryBatch`** (head-noun-last, matching the subject it travels on,
  `og.v1.telemetry.<node>`), and the storage write structs align with the `LogLineWrite` convention the log
  lane established: `MetricSampleEvent` and `StateSampleEvent` become **`MetricSampleWrite`** and
  **`StateSampleWrite`** (they are insert structs, not events), and `EventOccurrence` becomes
  **`EventWrite`**. Wire-safe by construction: protobuf encodes field numbers, never message names, so the
  bytes are identical before and after and a mixed-vintage node and server still interoperate. No behavior
  change, no schema change, no API surface touched.
