---
title: "The console follows state over its one live stream (#2731)"
date: 2026-11-26
refs: [2731, 2724, 1830, 2732]
---

The tab's one `/views:watch` stream now carries `changed` notices beside the view bindings' rows. A
health recompute (every alarm, slot and member write makes one) notes the system and component it moved,
and a pgx query tracer publishes the note on the tail's change lane when the transaction commits; a
watch's `topics` resolve in the caller's scope at connect and are coalesced to one notice each 250 ms.
The console's `liveQuery` (`web/src/lib/live.ts`) refetches on a notice, and the hub that owns the
stream reconnects with jittered backoff, closes in a hidden tab, and falls back to a slow refresh only
while no stream opens. The system page's health, slots, members and alarm history are the first reads off
their 5 s poll ([ADR-2731](/architecture/decisions/#adr-2731-the-console-follows-state-over-its-one-live-stream-and-polling-is-guarded-out)).

Every SSE route now sends `X-Accel-Buffering: no` and a write deadline per send, and the watch ends with
a final `mark` on shutdown. `web/src/polling-guard.test.ts` bars polling from `web/src`; the 26 files
still polling are listed in `web/src/polling-allowlist.json`, which only shrinks as their pages move.
Collection topics, for the list pages, are #2732.
