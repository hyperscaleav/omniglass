---
title: "The recording service is simulated and collected (#1363)"
date: 2026-11-02
refs: [1363, 1266]
---

The dev fleet's Fleet Recording Service was a `generic-service` the lab placed and collected nothing
from. It now has a simulator profile, a product and a driver of its own, so the lab shows how
Omniglass watches a software service: reachable over HTTPS, healthy by its own health endpoint, with
the counters a recording service reports.

The `recording-service` profile serves an HTTPS JSON API behind an api key header
(`X-Recording-Key`). `GET /api/health` answers a status, a version and an uptime; `GET /api/work`
answers the recordings in progress, the queue depth, the storage used and the last failure. The work
drifts as behaviors, each in a band. `POST /api/recordings/fail` fails a recording: it records the
failure and names it on the service's live event feed.

The new `recording-service-api` driver polls both routes every 30 seconds and holds the feed open.
It lands `service-state` and `software-version` (new properties), `active-recording-count` and
`queue-depth` (new metrics), and `storage-utilization`, the ratio the Boreal Cast player already uses,
beside `uptime`. A failure on the feed lands as a `recording-failed` event on the component carrying
the recording's id and the reason.

The `recording-service` product (Shinra Cloud, `generic-service` classification) carries the driver,
the dev fleet's recording service moves onto it, and the lab attaches the driver with a platform
secret holding the key. The service leaves `coverage.yaml`'s known-gap list. The issue asked for
`http-json`, which is the Tessellate sensor's driver (plain http, a census key, two polls); the
service needs https, three counters and an event, so, as for the Cast player (#893), it has a driver
named for its product (ruled on the issue).
