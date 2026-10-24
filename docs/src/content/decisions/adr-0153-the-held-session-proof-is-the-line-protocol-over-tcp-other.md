---
title: "The held-session proof is the line protocol over tcp; other held transports are their own issues"
date: 2026-10-23
status: "Accepted"
summary: "The stateful arc proves the held session on the line protocol over `tcp` (sessions, listeners, the shared reader); SSH, which #816 named as the session-lifecycle carrier, and the other held transports are deferred, each to its own issue (#1032 ssh, #1033 telnet, #1034 websocket, #1035 tls, #1036 mqtt, #1037 udp, #1038 SSE over http, #1039 modbus/tcp, #1040 bacnet/ip, #1041 knxnet/ip, #1042 ONVIF as a driver on http, #1043 the file sample with #1044 to #1047 for file-moving protocols), with server-hosted ingress at #1024; there is no serial transport, serial being reached through an IP-to-serial gateway over tcp (or tls or telnet)"
---

- **Date:** 2026-10-23 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [nodes](/architecture/nodes/)
- **Decision:** The stateful arc of #603 proves the held session on one transport: the line protocol
  over `tcp` (sessions, #816; listeners and the shared reader, #817), against the simulator's NVP port
  and end to end. #816's scope named SSH as the session-lifecycle integration carrier; the architect
  accepted the arc on the line protocol instead and deferred SSH. Every other held transport is its
  own issue under #603: #1032 (ssh), #1033 (telnet), #1034 (websocket), #1035 (tls), #1036 (mqtt),
  #1037 (udp), #1038 (SSE over http), #1039 (modbus/tcp), #1040 (bacnet/ip), #1041 (knxnet/ip);
  server-hosted ingress (syslog, SNMP trap, webhook) is #1024. **There is no serial transport**
  (architect's ruling): a serial device (RS-232, RS-485) is reached through an IP-to-serial gateway,
  which is the `tcp` transport (or `tls` or `telnet`, as the gateway speaks), so its driver is a line
  protocol like any other and the node never owns a serial port.
  **How a protocol is placed.** The first rule that fits decides. A protocol that rides a carrier
  we already have with only payload differences (http with any body, a tcp line protocol, a
  websocket, and udp datagrams once #1037 lands) is a **driver**: ONVIF is SOAP over http (#1042).
  A driver that needs something its carrier cannot do yet (a body format, an auth scheme, a stream
  mode, TLS) makes that **capability** on the existing transport, proven by the driver: SSE on http
  (#1038), tls (#1035), sftp on ssh (#1045), and http fetching a body as a file (#1044). A
  protocol that moves whole files rather than values lands as a **file sample** (#1043, the
  primitive #1044 through #1047 depend on). A protocol with its own framing, addressing, or session handshake is a
  **new transport** plus a generic driver on it, the `snmp` and `snmp-generic` pattern: ssh (#1032),
  telnet (#1033), websocket (#1034), mqtt (#1036), udp (#1037), modbus/tcp (#1039), bacnet/ip
  (#1040), knxnet/ip (#1041), ftp/ftps (#1046), and tftp on udp (#1047). A serial link is reached only through a gateway, as above. A
  protocol that pushes to us rather than being reached is server-hosted **ingress** (#1024), not a
  node transport. The `/scope-protocol` skill applies this placement rule when a new protocol comes up,
  and files the issue in the house shape. The session machine, its
  `session-state` surface (ADR-0150), and the routing (ADR-0151) are transport-agnostic behind the
  `SessionDialer` and `HeldConn` seams, so each of those issues adds a dialer and its own
  real-implementation integration test rather than a second machine.
- **Context:** The #603 rulings set the line-protocol emulator as the stateful proving target, with
  SSH to carry the session-lifecycle test. The line protocol exercised every part of the lifecycle
  (establish, arm, operate, recover, teardown) over a real wire, and SSH adds authentication and
  channel semantics that are a slice of their own; holding the arc for it would have delayed a proven
  machine. The collection and nodes pages' transport fences now name these issues, so no deferred
  transport is a fence without an owner.
