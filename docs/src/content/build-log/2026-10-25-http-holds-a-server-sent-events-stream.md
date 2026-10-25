---
title: "http holds a server-sent events stream (#1038)"
date: 2026-10-25
refs: [1038, 1113]
---

The third slice of the second protocol wave (#1113)
([ADR-1038](/architecture/decisions/#adr-1038-an-http-session-holds-a-server-sent-events-stream-and-a-held-connection-carries-messages)).
An http session may declare `session.stream`, a `GET` held open, and listeners claim its events by
the event name, a prefix or regex on the data, or a JSON field match (`internal/driver/message.go`).
The held connection's seam widens from a line to a message, so one router serves lines and events.

The collection engine's stream dialer (`internal/collection/httpstream.go`) is the session machine's
staged dialer: it connects (and runs the handshake over https), logs in first when the session
declares a login, sends the stream request with the scheme's credential, the login's session, the
cookies and the Last-Event-ID, and judges the answer's head; the reader feeds the WHATWG
event-stream interpretation (`internal/collection/sse.go`). A drop resumes from the last event id
after the server's retry, held to the backoff's bounds, and an event already landed is dropped. The
node holds an http endpoint's stream as its session, sharing the endpoint's learned auth and a
resume that outlives a recycle; a stream refusal gates the polls, a poll's refusal stops the stream,
and the polls wait for the stream's first verdict under a fingerprint. The registry's `http` and
`https` read `held`.

The simulator's http face may declare `events`, every change to its named paths one numbered event
replayed from a Last-Event-ID, behind the face's auth, with a console drop; the Kestrel bar streams
its call, volume, mute and power, and the seeded `kestrel-api` holds that stream beside its polls.

Proven by the event-stream interpretation's tests, over real sockets (events in order, a resume
after the server's retry with nothing lost or twice, a refused credential one attempt, digest
across a reconnect, a form login first and again on a lapse, an uncredentialed stream proving no
credential), in the simulator's face, and end to end (`run_http_stream_test.go`: a change on the bar
lands within a second with no poll; a dropped stream recovers and lands each change once; a wrong
password reads rejected with its reason, sent once under the fingerprint, across a restart; the fix
brings it back; a stream behind a form login logs in first).
