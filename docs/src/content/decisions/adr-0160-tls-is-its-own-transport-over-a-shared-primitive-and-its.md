---
title: "TLS is its own transport over a shared primitive, and its pin is the public key"
date: 2026-10-24
status: "Accepted"
summary: "`tls` is its own held line transport (rung `handshake`) over one TLS dial primitive the http, websocket and mqtt carriers consume later, so a component holds a raw port and a TLS one side by side (#1035). The verification mode is the driver input `tls-mode` (verify, pin, skip), the driver's default, verify when none, skip never a default. The pin is the public key (SPKI, the ssh `SHA256:` form), trusted on first use as `tls-certificate`, delivered back by the worklist, one per endpoint shared by its session and probe; a changed key or a failed verification is a terminal `certificate` refusal at the transport rung, the gate carrying the pinned and offered keys as data; `:retry` with `certificate` accepts a new key. The probe lands one `tls-handshake-time` per answered probe and, for an accepted certificate, `tls-certificate-expiry` and `tls-certificate-days-left`"
---

- **Date:** 2026-10-24 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [nodes](/architecture/nodes/), [glossary](/architecture/glossary/), [drivers](/guides/admin/drivers/), [simulation](/architecture/simulation/), [api](/architecture/api/)
- **Decision:** (#1035, under the architect's approval of epic #1076, decisions 1 to 4 and 7a.)
  1. **A distinct transport over a shared primitive.** `tls` joins the registry as a held line
     transport with the handshake as its rung: the line protocol rides it exactly as it rides
     `tcp`, one session machine, one reader, one routing. The handshake is one primitive in
     `internal/collection` (`HandshakeTLS`), which the http (https), websocket and mqtt carriers
     consume later as a capability on their own transport. **The placement rule:** a secure
     variant of a carrier that needs its own endpoint name and its own rung is its own registry
     entry over a shared primitive; one that needs neither is a capability on the carrier. This
     amends [ADR-0153](#adr-0153-the-held-session-proof-is-the-line-protocol-over-tcp-other-held-transports-are-their-own-issues),
     which placed tls as a capability on tcp. **Amended (#902):** https meets both tests (its rung
     is the handshake, and a component may serve a plain web port beside a TLS one), so it is its
     own registry entry over this primitive, not a capability on http
     ([ADR-0902](#adr-0902-http-polls-and-commands-send-real-requests-read-through-one-extraction-authenticated-by-a-declared-scheme));
     wss beside websocket follows the same answer (decision 4 of #1113).
  2. **The mode is a driver input.** `tls-mode` (`verify`, `pin`, `skip`) is derived into the
     endpoint's params as `host` and `port` are, so it edits in place (`PATCH /endpoints/{id}`
     `inputs`) and moves the connection fingerprint. The driver declares its default (the
     seeded `newtron-nvp-tls` pins, since its gear self-signs); a driver that declares none
     verifies; a spec defaulting to `skip` is refused at write, so skip is only ever an
     operator's choice, shown on the endpoint. Verify checks the chain against the node's
     system roots and the host input as the server name. TLS 1.2 is the floor.
  3. **The pin is the public key.** The fingerprint of the leaf's SubjectPublicKeyInfo, in the
     `SHA256:` form ssh fingerprints a host key in, so a renewal that keeps the key is no change.
     Pin on first use follows [ADR-0157](#adr-0157-telnet-and-ssh-are-held-line-transports-their-login-is-the-drivers-and-the-ssh-host-key-is-pinned-on-first-use)
     point 3: the node trusts the first key, publishes it as the observed `tls-certificate` (a
     node-only key the push lane refuses and no driver may emit), and the worklist delivers it back (the declared
     value first); the declared pin alone is a fingerprint input. The node keeps **one pin per
     endpoint, shared by its held session and its probe**, admitted atomically and only once the
     handshake completes, so neither trusts a key the other refused.
  4. **A refusal retrying cannot fix is `certificate`, at the transport rung.** A changed key and
     any verification failure (an unknown authority, a name mismatch, an expired certificate)
     reject with their reason, never a timeout; beneath the gate only the TCP connect runs, so no
     handshake (and so no login, poll, command or arm) reaches the new key, across a node restart
     too. For a changed key the gate value carries both keys as the evidence
     [ADR-0159](#adr-0159-a-refused-keys-evidence-rides-its-gate-and-trusting-a-key-is-checked-against-the-one-offered)
     gives a refused host key (`<rung> <class> <fingerprint> <pinned-key> <offered-key>`), and the
     reachability read serves them as `pinned_key` and `offered_key`, so the console's trust-this-key
     notice renders a certificate refusal unchanged and never parses a reason. An operator accepts it with `:retry` and `certificate`, or `host_key` (the
     key the endpoint's transport pins, the field a key refusal's trust action sends), gated
     `endpoint:retry`, held to ADR-0159's rule (the exact offered key while a refusal names one, else
     a 409), and audited, which declares the pin.
  5. **What the probe lands.** `tcp-open` and the connect time, `endpoint-responsive`, one
     `tls-handshake-time` per probe the component answered (a rung-specific name until #1063 folds
     probe timings into `rtt`), and, for a certificate the mode accepted, both
     `tls-certificate-expiry` (the not-after, a property) and `tls-certificate-days-left` (a
     metric, one observation per probe). A refused certificate lands no certificate fact.
  6. **The simulated face.** The panel serves NVP over TLS (`nvp-tls`) with a key derived from
     the simulated component's id, so it is the same on every run and a pinned endpoint survives a simulator
     restart; the profile sets the certificate's validity window.
- **Context:** #1035 and the `/scope-protocol` skill placed tls as an option on tcp. An endpoint's
  name is its transport and unique per component, so an option would have let a component hold
  only one of a raw port and a TLS one, and made the handshake rung a conditional default. A
  certificate pin rejects every renewal, even one that keeps the key; the key is what ssh pins
  too. An expression cannot read the clock, so a rule cannot turn a not-after into "fewer than 30
  days": the property is the fact and the metric is the number a threshold rule reads. A per-run
  simulator certificate would reject every pinned endpoint on each restart.
