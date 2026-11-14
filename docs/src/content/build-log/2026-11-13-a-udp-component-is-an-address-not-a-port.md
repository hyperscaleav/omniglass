---
title: "A udp component is an address, not an address and port (#1103)"
date: 2026-11-13
refs: [1103, 1037]
---

A udp component that answers or pushes from a different source port than the one it listens on now
reads responsive. The datagram reader accepted a reply only from the component's exact address and
port, so a peer with a separate send socket had every datagram counted as foreign and dropped, and
its request rung read `timed out`. With one socket per endpoint the port match protected nothing, so
the reader matches the address alone; a datagram from any other address is still counted, self-logged
by source and dropped. [ADR-0163](/architecture/decisions/#adr-0163-a-udp-endpoint-binds-its-own-socket-and-its-rung)
carries the amendment, and the drivers guide and collection page say so.
