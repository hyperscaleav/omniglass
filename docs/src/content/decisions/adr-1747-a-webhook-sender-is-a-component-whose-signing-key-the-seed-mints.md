---
title: "A webhook sender is a component whose signing key the seed mints"
date: 2026-11-10
status: "Accepted"
summary: "A scenario declares a webhook sender (a profile with a webhook face) in the same pushers list as a token pusher, and the seed makes it a component with an endpoint of the driver its profile names, a signing secret minted for that seed alone, and one identity binding per system of its site. The simulator reads the key and the bound rooms back through the node's device list, so the lab mints nothing for it. Diverges from ADR-1743's third point for senders only: a sender holds no bearer token and is no service principal"
refs: [1747, 1743, 1370, 1282, 1623]
---

- **Date:** 2026-11-10 | **Status:** Accepted | **Pages:** [simulation](/architecture/simulation/#a-pusher-pushes-on-its-own-cadence), [lab](/guides/lab/#scale-lab-scenarios)
- **Decision:** (#1747.)
  1. **A sender is declared as a pusher is.** A scale manifest's `pushers` entry whose profile has a webhook face is a sender; its `systems` may be omitted, which scopes it to every system of its site (a meeting platform account holds all of a site's rooms). The validator still refuses a profile with neither face.
  2. **The seed provisions it from the profile and the driver spec.** A profile's webhook face names its `driver` (a shipped webhook driver) and a `provision` route. The seed reads the driver's session credential, its secret type and its one listener identity key, and seeds the sender as a component with a platform-owned secret of that type (each field minted at random), a server-hosted endpoint of the driver taking that secret, and a binding per system on the identity key valued with the system's name. No seed code names a platform.
  3. **The simulator learns the rest from the device list.** The list already carried a webhook's path, scheme and secret to the simulator beside the node (ADR-1370); it now carries the bound identity values too, and the simulator adds each through the provision route, so every post names a room that lands on a system.
- **Context:** #1743 gave the scale lab token pushers, but its push lane carried no webhook receipts, so the measured corporate lab (#1623) exercised one of the push lane's two ways in. A signed webhook is the other.
- **Not chosen:**
  - **The lab mints the signing key, as it mints a pusher's token (ADR-1743).** A token is verified by hash, so the seed never needs the plaintext; a signing key is verified by recomputing the signature, so the server holds the plaintext sealed either way, and the simulator already reads it back through the device list. Minting it in the lab would carry a secret through one more environment variable for no gain.
  - **A sender as a service principal with a token.** A webhook is authenticated by its signature on the endpoint it names, not by a bearer, so a principal would grant a permission nothing checks.
  - **Count the webhook rows in a measure lane of their own.** Webhook posts land on the push lane with the bookings, as the ledger already accounts them (ADR-1696); a separate lane is out of scope.
