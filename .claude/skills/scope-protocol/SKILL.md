---
name: scope-protocol
description: "Use when a new device protocol, integration, or vendor API comes up (\"can we talk to X?\", \"add BACnet\", \"support this camera\"): decide whether it is a new transport, a driver on an existing transport, or an existing transport gaining a capability, then dedupe and file the issue in the house shape under the drivers epic. Prevents a driver being built as a transport (or the reverse) and protocol issues filed without a proving sim."
---

# Scope a protocol

Collection splits the wire from the meaning. A **transport** owns how bytes reach a
component and how replies come back; a **driver** is data (spec-is-data) that says what to
ask over a transport and how to read the answer. Getting the split wrong is expensive: a
driver built as a transport duplicates connection, session, and routing code; a transport
smuggled into a driver spec leaves the session machinery (ADR-0150) and the inbound router
(ADR-0151) unable to see it. The authority is the
[collection page](../../../docs/src/content/docs/architecture/collection.mdx) and the
transport registry (`internal/transport/transport.go`, generated into the docs).

## Decide: transport, driver, or capability

Ask in order; the first yes decides.

1. **Does it ride a carrier we already have, with only payload differences?** HTTP (any
   body format, REST, SOAP, GraphQL, SSE), a TCP line protocol (raw, or over tls), a WebSocket, or plain UDP
   datagrams (request and reply matched by shape; #1037, not built yet). Then it is a
   **driver**. Example: ONVIF is SOAP over HTTP; PJLink is a line protocol over TCP; a
   projector's ASCII control on a UDP port is a driver on udp. Check the transport registry
   for whether the carrier is built; a driver on an unbuilt carrier depends on that issue.
2. **Is it a secure variant of a carrier, needing its own endpoint name and its own rung?**
   An endpoint is named by its transport and unique per component, so a variant a component
   can expose beside the plain one (a raw control port and a TLS one), or whose transport
   rung differs (the handshake rather than the connect), is **its own registry entry over a
   shared primitive** ([ADR-0159](../../../docs/src/content/docs/architecture/decisions.md)).
   Example: `tls`, the line protocol over the TLS primitive in `internal/collection`, its
   rung the handshake. File it as a new transport that consumes the primitive.
3. **Does the driver need something the carrier cannot do yet** (a body format, an auth
   scheme, a stream mode, a framing variant, TLS where it needs neither its own endpoint name
   nor its own rung)? Then it is a **capability on the existing transport**, proven by that
   driver, filed as one issue: "the <transport> gains <capability>, proven by a <driver>
   driver". Example: XML plus WS-Security digest for ONVIF; SSE on http; TLS on http (https),
   websocket or mqtt, each consuming the shared TLS primitive; SFTP on ssh. A protocol that
   moves whole files (logs, diagnostics, screenshots) rather than values lands its result as
   a file sample (#1043).
4. **Does it have its own framing, addressing, or session handshake that no existing
   transport carries?** Binary PDUs, register or object addressing, a tunnel connect, a
   broker protocol. Then it is a **new transport**, plus a generic driver on top where the
   operator supplies addresses (the `snmp` / `snmp-generic` pattern). Examples: Modbus/TCP,
   BACnet/IP, KNXnet/IP, MQTT.
5. **Is it a physical link reached through a converter?** Serial (RS-232/422/485) is
   reached only through an IP-to-serial gateway, which is the tcp transport (or tls or
   telnet). No serial transport. Same reasoning for any field bus with an IP gateway: the
   gateway's IP protocol decides, not the bus.
6. **Does the component push to us rather than us reaching it?** Syslog, SNMP traps,
   webhooks: server-hosted ingress (#1024), not a node transport.

Discovery (WS-Discovery, Who-Is, mDNS, SSDP) is never part of a collection issue; it
belongs with discovery and is named out of scope.

## Dedupe

`gh issue list --repo hyperscaleav/omniglass --state all --search "<protocol> in:title,body"`
and scan the sub-issues of the drivers epic (#603). Extend an existing issue rather than
filing a sibling.

## File it

Native Issue Type `Feature`, labels `area:collection` and `area:node`, linked as a
sub-issue of #603. Title: `feat: the <name> transport` / `feat: the <transport> gains
<capability>, proven by a <driver> driver` / `feat: specs for the <name> driver`. The body
carries, in prose:

- **Why:** which components speak it and what operators get.
- **Scope**, naming each seam it touches:
  - session: held or not, its establish / recover / down behaviour (ADR-0150);
  - inbound routing: how a reply is matched to its request and how a push reaches a
    listener (ADR-0151); a JSON or field match where position is not enough;
  - polls, commands (how they settle: reply or read-back), listeners (how they arm and
    re-arm);
  - auth from the endpoint secret;
  - the reachability probe and the transport registry entry;
  - extraction: reuse the existing one for the payload format, never a second.
- **Sim:** the simulated face that makes the real-wire test possible. No sim, no issue;
  the environment risk is the point of a transport (the capability-wrapping carve-out).
- **Acceptance:** user-observable behaviours, including one failure case that must read
  as its real cause (auth refused, exception code, certificate changed), not as a timeout.
  A refusal retrying cannot fix (credential, host key, certificate) is classified terminal
  so the session reads `rejected` and stops dialing (#1049), gated at its ladder rung for the
  endpoint's connection fingerprint (ADR-0158).
- **Out of scope**, each with its home.

No em dashes, no attribution, head-noun-last names. Report the issue number and the
transport-or-driver verdict with the one-line reason.
