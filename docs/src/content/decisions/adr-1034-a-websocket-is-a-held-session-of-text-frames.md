---
title: "A websocket is a held session of text frames, and a reply is matched by a minted id"
date: 2026-10-25
status: "Accepted"
refs: [1034, 1113]
summary: "A `websocket` endpoint, and a `wss` one over the shared TLS primitive, is a held session (#1034, slice 4 of epic #1113) through the staged dialer every held session uses: its auth step logs in first when the session declares an http login, then sends `session.upgrade` with the session's scheme, the login's session and its cookies, a 401 or a 403 to it a terminal auth. A message is a text frame, routed by the line session's precedence (ADR-0151): an arm subscribes on every connection, a listener claims a push, and a request is a frame (a line, or a JSON body rendered injection-safe) whose `reply.correlate` names the field the node writes a fresh random id into, its answer the frame carrying the id back. Ping and pong keep the socket proven, a frame is bounded to 1 MiB and dropped past it, and the probe is http's"
---

- **Date:** 2026-10-25 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [nodes](/architecture/nodes/), [glossary](/architecture/glossary/), [drivers](/guides/admin/drivers/), [simulation](/architecture/simulation/)
- **Decision:** (#1034, under the architect's approval of epic #1113.)
  1. **Two registry entries, one held session.** `websocket` and `wss` (decision 4 of #1113: `wss`
     over the shared TLS primitive, its own entry so a component can serve a plain socket beside a
     TLS one) are held line transports: a standing listen task or any driver poll holds the
     endpoint's socket, and its polls, commands and listeners share it. `wss`'s pin is the
     endpoint's public key, trusted on first use, refused as `certificate` when it changes, and
     trusted again with `:retry` and `certificate`; the transports a key pin rides are read off the
     registry's TLS flag, not a second list.
  2. **The upgrade is the auth step.** `session.upgrade` is a `GET` (a path, the root when absent, a
     query and headers, never `Upgrade`, `Connection` or a `Sec-WebSocket-*` header the handshake
     owns, bar a subprotocol). The staged dialer's connect is the TCP connect (and the handshake over
     `wss`); its auth step logs in first when the session declares an http login (the login dialog
     of ADR-1114, its session shared with nothing else on the endpoint), then sends the upgrade
     built as every http request is, the scheme's credential (`basic`, `bearer` or `api-key`), the
     login's session headers and its cookies, and reads the answer through a 64 KiB head bound. A
     `101` that completes the opening handshake opens the socket. The upgrade is one request, so a
     `401` or a `403` to it is a terminal `auth` whether or not it carried a credential (the status
     table lets a request's `403` fault that request alone); a lapsed login, or a `403` to a login
     session the attempt did not just open, logs in once more and upgrades again, and only a `403` to
     a fresh login's session is judged; anything else is a failed attempt on the session's backoff.
     A terminal rule that names a status judges the upgrade alone, never a frame's text. `digest` is
     refused at validation, since it answers a challenge per request, and so is `reprobe`, since the
     auth step reads the endpoint's gate before it sends and a re-probe could only replay the
     refusal. No header the handshake owns may ride the upgrade, an api key or a login.
  3. **A frame is a message.** A text frame is the held connection's message (ADR-1038), offered to
     the listeners first and handed to an exchange by the line session's precedence: one router goroutine owns every data frame the
     node writes and draws an exchange's boundary as the line reader does, so a frame received
     before a request was sent is never its answer. The reader never fails the connection itself:
     its end travels to the router behind every frame read before it, so an answer, a terminal
     reply or a push the component sent before it hung up is routed first. A request is a frame: a `line` sent as is, or a
     JSON `body` rendered injection-safe as an http body is (a whole-leaf `${value}` keeps its
     type). An arm is a frame too, so it carries no line break (refused at validation, on every held
     line transport, since a node joins arms by line).
  4. **A reply is matched by a minted id.** `reply.correlate` names a field of a JSON object body (a
     dotted path of object keys, through objects, never a field the body sets itself). The node
     writes a fresh random id into it each time it sends the
     frame (a command's at execute, so a redelivery never reuses one), and the reply it waits for
     accepts only a frame whose field carries that id; its `error` shape answers only a frame
     carrying the id too, as the exchange's refusal. A reply may instead be a `prefix`, a `regex`, or
     a JSON `field` match (on any held line session), exactly one. A correlated or shaped exchange
     that gets no answer in time fails alone; an unshaped one recycles the socket.
  5. **Keepalive and bounds.** The node pings every 30 s and the socket is dead when it hears
     neither a frame nor a pong for two intervals. A frame past 1 MiB is read off the wire, dropped,
     and named unclaimed, and the socket reads on, so a hostile frame neither grows the node's
     memory nor wedges it in a reconnect loop; a binary frame is named unclaimed. The node sends no
     `Origin`; a driver that needs one declares it in `session.upgrade`'s headers.
  6. **The probe is http's.** A websocket endpoint's probe is a credential-free `GET /` (over the
     handshake for `wss`, `https`'s), any status an answer, so reachability stays observed beneath a
     refused upgrade and no probe-only key is added.
  7. **The simulated face.** A profile interface of protocol `websocket` serves a JSON control
     socket of its own, plain or under the device's https identity, behind the face's own static
     auth (`basic`, `header`, `bearer`, `query`): a subscribe acknowledged and then pushed every
     change to its paths, a get answered by a result carrying its id, a set on a settable path
     pushing the change before its result, and a console drop closing every socket going-away. The
     Kestrel bar serves one over wss on 9443, and the seeded `kestrel-ws` holds it.
- **Context:** #1034 predates the staged dialer, the gate, the fingerprint and the message seam;
  holding the socket as a line session keeps one router, one arm conversation and one terminal
  handling for lines, events and frames rather than a websocket-specific path. Correlation is the
  router's reply shape, so a request matched by its id needs nothing beyond a reply that accepts one
  frame; the id is random so a late answer to an exchange that gave up never answers another. Two
  choices diverge from a reading of the epic: the probe reuses http's rather than attempting an
  upgrade, since any http answer already proves the transport rung and an upgrade the credential
  gates would read the same as a 404; and a `403` to the upgrade is terminal, since the socket has
  no other request a `403` could be scoped to. A simulated websocket face does not serve a form or
  token login (its auth is a static mode on its own port); the node's login over a websocket is
  proven against real sockets instead. This slice completes epic #1113.
