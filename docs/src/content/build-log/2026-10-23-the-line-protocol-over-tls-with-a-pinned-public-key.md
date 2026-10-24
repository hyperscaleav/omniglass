---
title: "The line protocol over tls, with a pinned public key (#1035)"
date: 2026-10-23
refs: [1035]
---

The first slice of the protocol wave (#1076), under the architect's approval of its decisions 1 to 4
and 7a ([ADR-0160](/architecture/decisions/#adr-0160-tls-is-its-own-transport-over-a-shared-primitive-and-its-pin-is-the-public-key)).
`tls` joins the transport registry as its own held line transport, its rung the handshake, rather
than an option on `tcp`: an endpoint is named by its transport, so a component now holds a raw
control port and a TLS one side by side. The handshake is one primitive, `collection.HandshakeTLS`,
under one of three modes (verify against the node's system roots with the host as the server name,
pin the public key on first use, skip), reporting the offered key's SPKI fingerprint (the ssh
`SHA256:` form) and the not-after. The mode is the driver input `tls-mode`, derived into the
endpoint's params as `host` and `port` are and edited in place; the driver declares its default
(the new official `newtron-nvp-tls` pins), none means verify, and a spec defaulting to skip is
refused at write. The held session runs the handshake before its auth step through a `TLSDialer`
(`ConnectTLS`), so a session that declares tls over a dialer that cannot run one is a protocol
refusal, never plaintext. Polls and commands dispatch by the registry's line flag, and a tls poll or
command rides only the held session.

A changed key or any verification failure is a terminal `certificate` refusal at the transport rung,
naming both keys for a changed one and carrying them as data (`TerminalError.Pinned`/`Offered`). The
node keeps one pin per tls endpoint, shared by the held session and the probe, admitted atomically
once the handshake completes (`HostKeyPin.Admit`), published as `tls-certificate` on first use and
again while the worklist lacks it, replaced (recycling the session) when the worklist delivers a
different one. Beneath a `certificate` gate the probe runs only the TCP connect, across a restart
too. The gate value carries the two keys (`responsive certificate <fingerprint> <pinned>
<offered>`, the grammar ADR-0159 gives a refused key), and the reachability read serves them as `rejection.pinned_key`
and `rejection.offered_key` (as #1078 serves a host key's), so its
trust-this-key notice renders a certificate refusal unchanged; `:retry` takes the key through
`host_key` (now the key the endpoint's transport pins) as well as `certificate`, under ADR-0159's
409 for a key that is not the offered one on record. The
worklist delivers the pin as `CertificatePin` (declared first), the declared pin joins the connection
fingerprint (`fingerprint.Inputs.CertificatePin`, the reflection and table tests green), and
`POST /endpoints/{id}:retry` takes `certificate` (audited, a tls endpoint only, a `SHA256:`
fingerprint only; `omniglass endpoint retry --certificate`). The four tls keys are ruled node-only
(#1080's ruling, merged beside this slice), so the push lane refuses them. The probe lands the tcp pair, `endpoint-responsive`, one
`tls-handshake-time` per answered probe, and for an accepted certificate `tls-certificate-expiry`
and `tls-certificate-days-left`; the reachability read serves a tls row's mode, key, handshake,
expiry and days left, and the console's breakdown teaches them (amber inside a month) with
**unverified** on a skipped row. The simulated panel serves NVP over TLS (`nvp-tls`, 51326) with a
key derived from the simulated component's id, so a pin survives a simulator restart, its validity window the
profile's `tls_validity_days`; the dev fleet attaches `newtron-nvp-tls` to the huddle panel.

Proven over real sockets (`internal/collection/tls_test.go`: pin, verify with an unknown authority,
a name mismatch and an expiry, skip, plaintext retryable, the probe's facts, a held session rejected
for a changed key that then dials nothing over an hour of its clock), over fakes for the node's
pin, probe gate and restart (`session_tls_test.go`), against Postgres (`tls_pin_test.go`, the API
e2e), and end to end against the simulated face (`run_tls_session_test.go`: first use, poll,
command, push, recovery onto the same key, a changed key held beneath the gate with no handshake
across a node restart, the operator's accepted key, verify refusing the self-signed face as
`certificate` and skip connecting, each mode edit lifting the gate). The collection page's probe
table gains its tls row by hand; rendering it from the registry is #1087.

The review's fixes, each with a test that failed first: a driver may not emit a node-only key
(`tls-certificate`, `ssh-host-key`, `endpoint-rejection`, `session-state`, `session-reason`, and with
#1080's ruling every native probe key), refused at spec write, since a poll emitting the pin key would have had the worklist deliver a driver's
constant as the pin, bypassing the audited `:retry` (a lane open for `ssh-host-key` since #1032; the
catalog entry now covers every lane); a probe-only endpoint's certificate refusal self-logs its
reason once, since it has no `session-reason`; the reachability read serves the pinned key in pin
mode alone and withholds the last accepted certificate's expiry and days left while a certificate
gate holds (the probe then connects only, so they froze and read as current), the panel naming the
refusal instead; a bare tls endpoint's `tls-mode` param is checked at create and edit
(`ErrEndpointParamsInvalid`, a 422 naming the field); and the seeded `tls-certificate` description
says what the node publishes (first use, again while the server lacks it).
