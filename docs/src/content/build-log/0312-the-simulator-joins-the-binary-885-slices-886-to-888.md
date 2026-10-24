---
title: "The simulator joins the binary (#885, slices #886 to #888)"
date: 2026-10-18
refs: [885, 886, 888]
---

The device simulator that lived in the separate omniglass-lab project now ships in the omniglass
binary (`internal/sim`), so a fleet of fictional devices can be collected from without real gear
or a second stack. The first slice ports the engine: the embedded catalog of device and system
profiles, templates and fleet manifests; the fleet compiler; and per-device listeners for HTTP(S),
SNMP, SSH, Telnet, TCP line sessions and UDP over one live state per device. The lab's director,
gRPC stream, packet tagger and Zabbix trigger definitions stay behind, and the lab's word
"estate" follows the glossary to fleet.

Porting found what the lab's own tests had stopped seeing. They located the catalog by probing
the filesystem and skipped when the probe failed, so a template reference left behind by the move
to fictional vendors had gone unnoticed; they now load the embedded catalog and fail loudly, and a
test walks every template the catalog references. A stopped device kept answering clients that
were already connected, because each listener stopped accepting without closing what it had
accepted; the acceptance test failed every second run in the same process until stops closed
their connections. And with state no longer round-tripping through a database, integers stayed
integers, which a DSP template formatted as floats.

The second slice puts the simulator beside a node. `omniglass sim run` holds the node's own
credential, so the node's subject grant is the whole of its authority: two new subjects in that
grant carry its device-list pull (`og.v1.simlist.<node>`) and its hit batches
(`og.v1.simhit.<node>`). It serves each component placed on the node whose product names a profile
and whose interfaces target a loopback address, and follows the list without a restart. The server
keeps hits in memory, accepting them only for components it handed that node, and shows them at
`GET /nodes/{name}/simulation` (`omniglass node simulation`). Writing the end-to-end test showed a
collector's reachability probe never registered, since it connects and closes without a request;
an accepted connection now counts as a hit. The Helm chart has no node to put a sidecar beside
yet (#892).

The third slice simulates the seeded inventory, so the dev fleet is collected for real. The dev
seed gives every component a simulator profile serves an address and endpoints on the dev node:
the SNMP products attach `snmp-generic` (a seeded community secret, which meant `seed-dev` now
loads the server's secret key), the Newtron panels attach `newtron-nvp`, and products whose drivers
are still stubs get a reachability probe. `make dev-sim` runs the simulator and the node together.
Run live, all 122 simulated components were served and reached, each landing what its driver
collects (uptime and an observed model number over SNMP, the observed video input over the line
protocol), and `set-input` issued to a panel changed the simulated device. Profiles share only
between variants of one kind of device (the Boreal Edge sizes), held to that by the seed's
component types; the touch panel gained the NVP port, the display and DSP answer `sysDescr`, and
the port shift lifts only ports below 1024. The 1,000-device benchmark found a device with two line
protocols serving both with one's config. Run live, `set-input` actuated but never settled: the panel is polled
every 30 seconds against a 15-second settle window, and settlement is only checked at issue. The
fix, settling from the command's response or the next observation as each binding chooses, is
#900.
