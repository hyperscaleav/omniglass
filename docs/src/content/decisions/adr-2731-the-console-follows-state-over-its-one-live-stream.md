---
title: "The console follows state over its one live stream, and polling is guarded out"
date: 2026-11-26
status: Accepted
summary: "Each console tab holds one /views:watch stream. Beside the view bindings' rows it carries changed notices for the topics the page follows (a component or a system), published on the tail's change lane after the write commits, resolved in scope at connect, and coalesced per topic. A notice is an invalidation: liveQuery refetches the typed route. A vitest guard bars polling from web/src, with a list of the files still polling that only shrinks. Diverges from the issue: the notices ride the tail's ring as a lane, not a sibling ring, and collection topics wait for a later slice."
refs: [2731, 2724, 1830, 2732]
---

- **Date:** 2026-11-26 | **Status:** Accepted | **Pages:** [UI](/architecture/ui/), [API](/architecture/api/), [views](/architecture/views/)
- **Context:** The console polled: 48 `refetchInterval` reads across 27 files, every 1 to 60 s. A
  component page made about 90 requests a minute, each an auth query and a data query, nearly all
  answering "nothing changed", and showed data up to 5 s old. ADR-1830 had already given each tab one
  multiplexed stream, carrying only ingest rows.
- **Decision:**
  1. **One stream per tab, widened, not a second one.** `/views:watch` takes `topics` (comma separated
     `kind:ref`, a component or a system, at most 32) beside `bindings`, and either alone is a valid
     watch. A `changed` event names a topic a write moved:
     `{"topic":"system:<ref>","kind":"system","id":"<uuid>","op":"update","v":<seq>}`. It carries no data:
     the client refetches through the typed route it already reads, so authorization, redaction and the
     response's shape stay on that route. `/live` was not added: the watch already had the cursor, the
     resume, the scope and the per-user cap.
  2. **The change lane rides the tail.** A notice is a row on the tail's ring (`tail.LaneChange`), not a
     ring of its own, so a stream has one position: its event id resumes the bindings and the topics
     alike, and a position the ring no longer holds is a `reset` (with `topics: true`) for both. A
     notice publishes after the transaction that wrote it commits, never before: the write chokepoint
     notes the entity on its transaction's connection, and a pgx query tracer on every pool connection
     publishes what that connection noted when it sees the commit, and drops it on a rollback. That is
     the one place every transaction's commit is visible, whoever began it. In-process, as the tail is;
     not Postgres `NOTIFY`, which takes a commit-time global lock under write load.
  3. **The chokepoint is the health recompute.** Every write that moves a system's health, its slots or
     its members recomputes the system, so the recompute names the system (and each component it
     recomputes), whether or not the verdict moved: a slot assigned to a healthy member changes the
     page without changing the health.
  4. **Scope at connect, never a fleet-sized set.** Each topic needs its kind's read and resolves inside
     that read's scope at connect, as a binding's literal ref does: one out of scope is the same 404 as
     one that does not exist, so a stream never names an entity its caller cannot read. A stream holds
     the topics it named, not its caller's scope.
  5. **Coalesced per topic.** A stream names a topic at most once each 250 ms: the first notice goes
     out at once, the rest of a burst waits and goes out as the newest at the window's end. While one
     waits, the rows behind it wait too, so no event's id moves the client past a notice it has not had;
     a reconnect from any id misses no change and repeats none.
  6. **The console resumes from `topics_cursor`.** A reconnecting EventSource's `Last-Event-ID` resumes
     the topics, but the console passes `topics_cursor` instead, because `Last-Event-ID` would also move
     a newly joined binding past its history's cursor. A stream opened with no position refetches what
     was read before it opened, the one window a notice could not cover.
  7. **The hub owns the connection.** `liveQuery` (`web/src/lib/live.ts`) is `useQuery` with `topics`,
     refetching on a notice (debounced 250 ms) and on a reset. The hub reconnects with jittered
     exponential backoff (1 s doubling to 30 s), closes the stream in a tab hidden 30 s and resumes it on
     focus, and refreshes every 30 s only while no stream delivers (a refused open, the per-user cap, no
     first event within 10 s behind a buffering proxy).
  8. **Stream hygiene.** Every SSE route sends `X-Accel-Buffering: no` and sets a 10 s write deadline
     before each send, so a client that stops reading ends the stream rather than parking its goroutine.
     The watch also sends `retry:` and, on server shutdown (`RegisterOnShutdown`), ends each stream with
     a final `mark`. The omnibot run is a POST no EventSource reconnects and AG-UI has no mark, so it
     takes the header and the deadline and keeps its own end.
  9. **The guard.** `web/src/polling-guard.test.ts` fails on `refetchInterval`, `setInterval`,
     `new EventSource`, and a `setTimeout` that reads or re-schedules itself, outside `lib/clock.ts`
     (the UI tick) and `lib/live.ts`. `web/src/polling-allowlist.json` names the files still polling: a
     file not on it that polls fails, and one on it that no longer polls fails, so the list only shrinks.
- **Divergence from the issue:** the issue drew the change hub as a sibling of the tail ring; one ring
  keeps one cursor per stream (point 2). Collection topics (a scoped list, at most once a second) are not
  in this slice: the system page needs entity topics only, and #2732 tracks them.
- **Consequences:** the system page's health, slots, members and alarm history no longer poll; the
  remaining pages move in later slices, most requests saved first, each taking its files off the list.
  A second server does not see the first's notices, as with the tail. The heartbeat still makes one
  auth query a stream each 15 s; an in-memory access epoch is a follow-up.
