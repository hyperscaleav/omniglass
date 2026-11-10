---
title: "An operator switches a PDU outlet, and switching it off holds what it powers (#1653)"
date: 2026-11-10
refs: [1653, 1648, 1651, 1252, 1655]
---

The boot seed ships the first power commands. `outlet-off` is disruptive (quiet 15 s, hold 300 s)
and names `outlet` as its `power_outlet_param`, so its delivery holds every endpoint of what that
outlet feeds, out-of-band included, through the dependency edges of #1648; `outlet-on` takes
nothing down. Both are fire-and-forget and refuse params without an `outlet` from 1 to 4 at the
issue. The simulated Ferrite PDU 4C gains a control face, a POST to its `/api/outlets/state` behind its basic
auth, which switches one outlet so the next SNMP read and `/pdu.json` both show it and the outlet's
current drifts to 0; a bad outlet, a bad state or a wrong password moves nothing. A new shipped
driver, `ferrite-pdu-api`, carries both commands over that face beside `ferrite-pdu-snmp`, which
keeps reading the outlets, and the lab attaches it to the boardroom's power conditioner, whose
outlet 2 feeds the amp.

Tests: the sim switches and refuses (`TestFerritePDUSwitchesAnOutlet`,
`TestFerritePDURefusesABadSwitch`, `TestFerritePDUDocumentIsReadOnly`), the seed ships the pair
(`TestSeedCommandTypesShipTheOutletCommands`), the driver renders and switches the simulated PDU
(`TestTheShippedFerritePDUAPIDriverSwitchesAnOutlet`), against Postgres an outlet-off on outlet 2
holds the fed codec's endpoints and none of the PDU's or outlet 3's
(`TestOutletOffHoldsWhatTheOutletFeeds`, `TestOutletOnHoldsNothing`), and over the real wire an
outlet-off issued through the API switches the simulated outlet, a run now on the fed sensor is
refused as disrupted, and the PDU's next status poll reads `outlet-2-state` off
(`TestSwitchingAnOutletOffHoldsWhatItFeeds`).

Not yet: settling an outlet command against the outlet's read-back state waits on a driver naming
an instance (#1252); the simulated PDU powers nothing, so a fed device keeps answering and its hold
ends on its first up verdict after quiet; outlet cycle and SNMP SET are out of scope.
