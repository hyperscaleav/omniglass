---
title: "http polls and commands parse json, xml and html (#902)"
date: 2026-10-24
refs: [902, 1113, 1115]
---

The first slice of the second protocol wave (#1113), under the architect's approval of its
decisions 1 and 4 to 7 ([ADR-0902](/architecture/decisions/#adr-0902-http-polls-and-commands-send-real-requests-read-through-one-extraction-authenticated-by-a-declared-scheme)).
A driver's http request declares `method`, `path`, `query`, `headers`, a `body` and the response
`format`, validated at write (a path, a known method and format, no body on a GET, no credential
header, no secret in a template, a poll rendering nothing) and rendered injection-safe
(`internal/driver/http.go`). The one `Extract` primitive gains a namespace-aware `xpath` over xml
and parsed html (`internal/driver/xpath.go`, antchfx), one value per selector, each emit's selector
bound to the declared format. The session block declares `scheme` (basic, bearer, api-key, its
placement on the driver) over the new `bearer-token` and `api-key` secret types; `terminal` rules
may match a status. `https` joins the registry over the TLS primitive (the registry's `tls` flag
drives the tls mode, the pin and `:retry` with certificate for both), http stops claiming `held`,
and a listener on a transport with no listen path is refused at write (#1115).

The node's http fetcher (`internal/collection/httpfetch.go`) sends one request with the credential
applied, over plain http or the shared handshake, never following a redirect and capping the body,
and the status table (`JudgeHTTPAnswer`) splits a gating refusal, a request's own fault (which waits
its interval) and a retryable answer (which backs off at least its `Retry-After`). A query key is
redacted from every URL named and the credential scrubbed from any reason. A credentialed poll or
command sends at the session rung, so a refused credential gates both, and its answer lands
`endpoint-authenticated`; the probe stays credential-free and, over https, runs the handshake as its
rung, landing the handshake time and the certificate facts. An http command's rendered request rides
the command wire whole, its credential read off the node's worklist. The seed ships `kestrel-api`
(https, basic: system, call and audio polls; set-volume, set-mute and set-power) and `http-json`
(http, a header key: status and occupancy), the keys and command types they read and drive, and the
simulated fleet attaches both. The simulator's http faces gain bearer and query-key auth, an https
face serves an ECDSA key derived from its component's id, and the DSP serves namespaced XML.

Proven in the driver (validation, rendering, xpath over xml and html, the terminal rules, the
credential's resolution), in collection over fakes (`JudgeHTTPAnswer`'s table, the scrub, the
request's shape and scheme) and over real sockets (`httpfetch_integration_test.go`: the request and
its key, a redirect, the body cap, a query key never named, verify, pin and skip, the https probe),
on the node over fakes (`http_test.go`: the session rung, a command's credential off the worklist
and its gate, the bookings), in the simulator (the bearer and query faces, the https key lasting
across a restart), against Postgres (`https_pin_test.go`), and end to end (`run_http_test.go`: the
bar polled over https and actuated three ways, each settled from its reply, a replaced key held
beneath the gate across a restart until trusted, verify refused and skip connecting; a wrong header
key refused once per fingerprint across a restart and a rotation bringing it up; a bearer token over
namespaced XML and a query key over an HTML status page).
