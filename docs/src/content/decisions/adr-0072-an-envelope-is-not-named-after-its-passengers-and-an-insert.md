---
title: "an envelope is not named after its passengers, and an insert struct takes the Write suffix"
date: 2026-07-31
status: "Accepted"
summary: "Two naming rules: a carrier is named for what it carries, never a passenger (the telemetry wire message is `TelemetryBatch`, since it carries samples, log lines, and later events), and a storage insert struct takes the **`Write`** suffix paired with the bare read struct (`MetricSampleWrite` / `MetricSample`), the pattern `LogLineWrite` set. Retires `MetricSampleEvent`, `StateSampleEvent`, `EventOccurrence`, and the proto `Event`"
---

- **Date:** 2026-07-31 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/), [glossary](/architecture/glossary/)
- **Decision:** Two naming rules, both general. **A carrier is named for what it carries, never for one of
  its passengers.** The telemetry wire message is a **`TelemetryBatch`** (`proto/og/v1/telemetry.proto`): it
  carries samples and raw log lines and will carry natively caught events, so naming it `Event` named it after
  one passenger. **And a storage insert struct takes the `Write` suffix**, paired with the bare read struct:
  `MetricSampleWrite` / `MetricSample`, `StateSampleWrite` / `StateSample`, `EventWrite` / `Event`,
  `LogLineWrite` / `LogLine`. A `Write` is the shape a caller hands the gateway; the bare noun is the row that
  comes back.
- **Context:** "Event" had come to mean four different things: the wire batch, the two sample insert structs
  (`MetricSampleEvent`, `StateSampleEvent`), the event insert struct (`EventOccurrence`), and the event read
  struct. Three of the four collided in a single signature, `deriveSamples(ev *ogv1.Event, ...)
  ([]MetricSampleEvent, []StateSampleEvent, []EventOccurrence)`, where only the last had anything to do with an
  event as the platform defines one (an `event_type`-registered occurrence carrying an `origin`, ADR-0066).
  The batch had already outgrown its name once when the log lane added raw log lines to it, and the push route
  ([#423](https://github.com/hyperscaleav/omniglass/issues/423)) would have made the collision visible on the
  wire as `Event.events[]`.
  The rename was **free**, which is why it happened before the payload grew rather than after: protobuf encodes
  field **numbers** and never message names, so the same payload marshals byte-identically before and after
  (verified by encoding one on each side and diffing the hex), and a node and a server of different vintages
  still interoperate. No migration, no dual-write, no deploy ordering.
  The `Write` half is not a new idea, only a newly stated one: the log lane set the pattern with `LogLineWrite`
  in [#414](https://github.com/hyperscaleav/omniglass/issues/414) and the older types simply predated it, so
  the codebase carried two spellings of the same concept. Recorded here because it is a rule for **every future
  insert struct**, and a convention that lives only in a commit message is one nobody applies.
  ADR-0037's title still reads "a protobuf Event", correctly: that is what was decided on 2026-07-07, the log
  is append-only, and its heading generates the anchor other entries cite. It carries a forward pointer to the
  rename instead.
