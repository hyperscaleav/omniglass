---
title: "The simulated fleet in the console, and the simulator documented (#885 rollup 2, slices #890 and #891)"
date: 2026-10-18
refs: [885, 890, 891]
---

The simulated fleet gets its surfaces. `GET /simulation` is every simulator and its devices,
`GET /simulation/profiles` the catalog, and `GET /components/{name}/simulation` a device's API surface
read from its profile (every protocol surface with its routes, commands and OIDs, ports as the
simulator binds them). `POST /components/{name}/simulation:fire` sends one operation over the bus to the
simulator beside the component's node, on a new subject of the node's own grant (answered under a
one-reply response permission), and the device answers in process from its live state, so a command's
change sticks and the fire lights up its row; the real-simulator e2e fires through the CLI and sees the
answer and the hit. `POST /nodes/{name}/simulation/components` adds a simulated component through the dev seed's planner,
promoted to `internal/simfleet`, and the simulator serves it on its next pull. The console shows a
Simulation page (a card per simulator, a table of its simulated components whose rows light up on a hit), a simulation drawer (status,
impairment controls, the API surface and a fire form), and a Simulation section on a simulated
component's page, all hidden while no simulator is connected. The docs gain the operator's guide
(simulate a fleet), the install guide's simulator privileges (ports below 1024, ping, `NET_ADMIN`, macOS
aliases), and ADR-0142 for the run mode. The profile vocabulary meets ADR-0134 on the way: a profile's
"interfaces" are its protocol surfaces wherever an operator reads them, and the fire body names a
`surface`.
