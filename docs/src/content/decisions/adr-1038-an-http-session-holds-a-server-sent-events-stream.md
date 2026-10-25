---
title: "An http session holds a server-sent events stream, and a held connection carries messages"
date: 2026-10-25
status: "Accepted"
refs: [1038, 1113]
summary: "An http session may declare `session.stream` (#1038, slice 3 of epic #1113): a GET held open as the endpoint's session through the staged dialer every held session uses, its auth step the session's scheme or login (shared with the endpoint's polls and commands), its events offered to the listeners. The held connection's unit widens from a line to a message (a line, an event, later a frame), and the router gains an event-name match and a JSON field match beside prefix and regex. A drop resumes with Last-Event-ID after the server's retry, held to the session backoff's bounds, and an event already landed is dropped. A stream refusal gates the polls, a poll's refusal stops the stream, and the polls wait for the stream's first verdict under a fingerprint"
---

- **Date:** 2026-10-25 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [nodes](/architecture/nodes/), [glossary](/architecture/glossary/), [drivers](/guides/admin/drivers/), [simulation](/architecture/simulation/)
- **Decision:** (#1038, under the architect's approval of epic #1113.)
  1. **The stream is declared in the session block.** `session.stream` is a request: a `GET` with a
     path, a query and headers (never `Accept`, `Last-Event-ID` or `Cache-Control`, which the node
     sends), no body, its events read as `json` (the default) or `text`. It rides the session's
     scheme or login, as every request does, and it needs a listener; a listener over http needs
     the stream and declares no `arm`, since a stream has no upstream.
  2. **A held session through the staged dialer.** A standing listen task holds it: the connect is
     the TCP connect (and the TLS handshake over https, its pin the endpoint's), the auth step logs
     in first when the session declares a login, then sends the stream's GET with the scheme's
     credential, the login's session, the cookies and the Last-Event-ID, and judges the answer's
     head by the status table (a 401 is a terminal `auth`; a lapsed login logs in once more; a
     digest challenge is answered over a new connection); a 200 carrying `text/event-stream` opens
     the reader. Anything else is a failed attempt on the session's backoff. `session-state` and
     `session-reason` land as for a line session; `endpoint-authenticated` lands only for a stream
     that carries a credential. The registry's `http` and `https` read `held`.
  3. **A message, not a line.** The held connection's seam (`Inbound`) carries a message: a line,
     or an event with its data, its event name and its id (later a websocket frame). The router
     offers each to the listeners first (ADR-0151, unchanged), and a listener's match may name the
     event and carry one data rule: a prefix, a regex, or a **JSON field match** (a dotted path that
     equals a value or matches a regex). The field and the event name are the router's: a terminal
     rule, a login's ready and a settle keep a prefix or a regex. A claimed message is read through
     the one extraction primitive in its format. Slice 4 extends a reply shape with the field match
     for its correlation.
  4. **Resume.** The event-stream interpretation is the WHATWG standard's. A dropped stream
     reconnects with the last event id it was sent, after the server's `retry` held to the backoff's
     bounds (250 ms to 30 s); an event whose own id the stream already landed (the last 1024
     remembered) is dropped, so a replay duplicates nothing. The resume lives with the endpoint in
     node memory, across a session's recycle, never persisted. A line or an event past 1 MiB ends
     the connection.
  5. **Beside polls, through one gate.** The endpoint's polls and commands stay requests of their
     own and share the stream's learned auth (one login serves all). A stream refusal gates them; a
     refusal a poll or a command meets stops the stream, which restarts rejected under the gate; and
     while the stream makes its first attempt under a fingerprint, the polls wait for its verdict,
     so a wrong credential is sent once, by the stream.
  6. **The simulated face.** An http face may declare `events`: every change to its named state
     paths is one numbered event, the last 256 kept, replayed from a reconnect's Last-Event-ID (the
     event itself included, as some servers resend it), behind the face's auth, with a console drop.
     The Kestrel bar streams its call, volume, mute and power, and the seeded `kestrel-api` holds it.
- **Context:** #1038 predates the auth gate, `rejected` and the fingerprint, which the stream now
  obeys; its "http-json extraction" is the one primitive in `internal/driver`. Widening the seam to
  a message keeps one router for lines, events and slice 4's frames instead of a router per carrier,
  and declaring the stream in the session block keeps it one fingerprint input beside the scheme or
  login it rides.
