---
title: "The scale lab brings a scenario up with a node per site (#1554)"
date: 2026-11-07
refs: [1554, 1357, 1553, 1563, 1556]
---

The second slice of the scale lab: `omniglass lab run --scenario corporate --systems N` brings the
resolved plan up instead of only printing it. The lab's runner (`internal/lab`) gained a scale path
beside the learning lab's: the same network, Postgres, seed one-off and server, then one node
container per site, each enrolled with a token minted for it this run and simulating its site's
components in process. The seed one-off takes `--systems`, creates each site's node, and calls
`devseed.SeedPlan` (#1563), whose `PlanOptions.NodeFor` puts each site's endpoints on that site's
node. Every component answers on its own loopback address, and the lab is ready once each site's simulator lists all of the site's components and each of them
reads up (the planted faults aside). A scale lab is its own throwaway project, `omniglass-scale`.

What was extended rather than written anew: the lab's plan carries a list of site nodes in place of
its one node, the reachability read that judges a lab ready is shared by the learning lab's wait and
the per-site wait, the hold watches every site's node, and the CLI's Docker connection and runner are
shared by both kinds of lab.

Before it starts anything, the lab estimates what a size needs from a cost model measured on this
branch with `docker stats` at 20 and 100 systems: about 170 MiB and a twentieth of a core for the
server and Postgres, 36 MiB and a hundredth of a core for each site's node, and 0.9 MiB and a
thousandth of a core for each component. It sets that against 80% of what the Docker host reports and
refuses a size that does not fit, naming the largest that does, found by a binary search over the
scenario's sizes. `--plan` prints the estimate and the decision. A hidden `--host-memory` forces a
lower limit, to see a refusal without a small machine.

The bring-up is bounded (20 minutes by default, the hidden `--ready-timeout` otherwise), the seed
included. Past it the lab names the sites whose simulators never connected and each site's
components not up, by name and loopback address, since a component's name is unique only within its
system, and removes all it started.

`make scale-lab` runs `scripts/scale-lab.sh`, which brings a size up as a person would and asserts one
node container per site, every component on its own loopback address, and a teardown that leaves no
container or network. It is outside `make test` and CI. On the 12-core, 32 GiB worker, 100 systems (3
sites, 322 components) came up in 24 seconds. Bringing up 1,000 systems and more, and measuring a
running lab, is the harness's (#1556).
