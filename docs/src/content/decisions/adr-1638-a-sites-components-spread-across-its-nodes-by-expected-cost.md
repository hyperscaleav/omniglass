---
title: "A site's components spread across its nodes by expected cost, guarded by measured headroom"
date: 2026-11-09
status: "Accepted"
summary: "Accepted before any of it is built (#1624). A site with several nodes spreads its unpinned components across them. The unit is the component, all its node-run endpoints on one node; a server-hosted push endpoint stays outside it. The pool is the nodes at the component's location or its nearest ancestor with any, later a cascaded node-pool property (#1351). Every node in a pool must reach every device in it, and shared-port pushes (syslog, traps) land on any node in the pool, claimed by source address; a VIP across the pool is the operator's. An endpoint's node_name is a pin that wins. A server reconcile balances on expected cost (runs a second, the sum of 1 / interval, with a listen task at a flagged weight), sticky, with soft affinity over dependency edges and a hysteresis threshold. A node is saturated, and takes nothing new, when its schedule loss ratio or a pool's busy fraction from its latest report window is over a ceiling. A lost node's components move after the silent window plus a grace; a join fills gradually; a move between live nodes is two-phase and completes only on the old node's confirmation, so no device sees two sessions from live nodes. The worklist wire gains only a pull header naming the generation the node runs. Slices #1686 to #1690; the measured two-node baseline is #1690."
refs: [1638, 1624, 1637, 1686, 1687, 1688, 1689, 1690, 1625, 1634, 1659, 1641, 1648, 1649, 1655, 1351, 1586, 1094, 1577, 1024, 57]
---

- **Date:** 2026-11-09 | **Status:** Accepted | **Pages:** [nodes](/architecture/nodes/#load-distribution-across-a-sites-nodes), [scaling](/architecture/scaling/)
- **Decision:** (#1638, under #1624. Recorded before any of it is built, on top of #1637's concurrency model, so the slices build to one design.)
  1. **The unit is the component.** Every node-run endpoint of one component runs on one node, enforced by the Storage Gateway (#1687). The node's lanes are per endpoint (#1637), but a component's endpoints share a device, the driver's component-wide exclusion opt-in (#1649) and the endpoint gate's default hold set (the target component's endpoints). Splitting a component would put two sessions on one device and split one gate across two nodes. A push endpoint (a webhook, ADR-1024) is hosted by the server and takes no node: it never inherits the component's pin or assignment, adds nothing to its cost, and its listen tasks never enter a node's worklist.
  2. **The pool is the location.** A component's candidate nodes are the claimed nodes at its location, or failing that at its nearest ancestor location that has any. A child component with no location of its own (a component's location is not inherited from its parent today) takes its nearest located parent's. A component with no node up its chain stays unassigned and says why. Once #1351 lands, a cascaded `node-pool` property can widen, narrow or name a pool at any level, with the location pool as its default. The pool's one prerequisite is that every node in it can reach every device in it; assignment does not check it.
     - **Pushes land on any node in the pool.** Shared-port pushes (syslog, SNMP traps, tcp and udp push; #1148, #1149, #1150) do not follow the assignment: every node in the pool runs the pool's push listeners and claims each line by source address, so a device may send to any pool node. Failover for a single push target is the operator's virtual IP across the pool, not omniglass's. A shared-port listener is outside the component's unit and cost; a listener on a session a node holds stays with the assigned node. The edge is the default home for these pushes, since much of that traffic is unencrypted and should not cross public ingress; the server hosts webhooks and other standard public web traffic (ADR-1024), and server-hosted shared-port listeners (#1158) are the exception for a sender with no site node near it.
  3. **An endpoint's `node_name` is a pin.** It pins the whole component, and assignment never moves it. The server's assignment lives in its own column beside the pin, so the reconcile never overwrites an operator's choice. Deleting a node returns its assigned components to the pool (`on delete set null`); its pinned endpoints keep today's cascade.
  4. **Expected cost balances; measured headroom guards.**
     - The reconcile balances on a component's expected cost: the sum over its tasks of 1 / interval in seconds. A poll counts at its own interval and a reachability probe at the default probe interval (#1634). A listen task has no interval, so a held session or a listener counts at a flagged weight, default one 30 s poll; a listen-only component never costs 0, which would stack every such component on one node. It is pure, known before any node runs the work, and does not move when the assignment does.
     - A node is **saturated** when its schedule loss ratio (#1659 with #1641's schedule hop) is above a ceiling (flag, default 0.01), or when any pool's `worker-busy-fraction` (#1686) is above a ceiling (flag, default 0.8). A saturated node takes no new component. The loss ceiling is not 0, so one late tick does not close a node to a lost peer's components.
     - The guard reads only values from the node's latest report window. A window with no runs publishes no loss ratio (#1659), so a node that sheds or has not yet taken work would otherwise be held saturated by a stale value; a missing value counts as unsaturated.
     - The loss ratio keeps the uniform reading, 0 = healthy. The busy fraction is a capacity level, not a loss, and reads the same for every pool kind.
  5. **Assignment is a server reconcile** (#1688): read state, run a pure `assign`, apply the moves in one transaction, logging each move on both nodes. The rules:
     - **Pins win.**
     - **Sticky.** A component stays on its node while that node is in its pool and unsaturated.
     - **Placement.** An unassigned component goes to the least-loaded unsaturated pool node, ties broken by fewer components, then by node name. If a dependency-edge neighbour (power, host or path, #1648) sits on an unsaturated node, the component goes there.
     - **Rebalance.** Moves happen only when the most loaded node exceeds the pool mean by a hysteresis factor (default 1.25), and at most a capped number per pass.
  6. **Rebalance on loss and join** (#1689).
     - **Loss.** A node is lost only after the node-silent window (#1094) plus a grace period, default 120 s. Its assigned components then spread over the unsaturated peers. What the peers cannot take stays unassigned, and the site reads as under capacity. Pinned components stay. A lost node still running, cut off from the server but not from its devices, keeps its last worklist (#1577), so a moved component can see two sessions until it reconnects; that is accepted over leaving the site unpolled.
     - **Join and return.** A joining or returning node starts empty and fills under the per-pass cap, with no give-back.
     - **Two-phase moves.** A move between live nodes is two-phase: the old node drops the tasks and closes its session on its next pull, and the new node is assigned only once the old node confirms it. The confirmation is the old node's pull request naming a worklist generation at or past the clear. A served reply is not proof: a pull can time out at the node after the server answers, and the node keeps its last worklist (#1577). A move unconfirmed at its deadline is abandoned and the component returns to the old node.
  7. **The worklist wire is unchanged** but for one pull request header naming the generation the node runs (6). A move reaches both nodes on their next pull (30 s, jittered); #1586's nudge shortens that later. The worklist generation moves on both nodes on every move, because the max `updated_at` of the remaining endpoints may not.
  8. **Independent of server replicas.** Assignment maps components to nodes, and any server answers any node's pull. The reconcile is a scaling.md singleton: one by construction while there is one server, then behind the NATS KV lock (#57).
- **Options weighed:**
  - **Static pinning only (today).** An operator names each endpoint's node.
    - For: simple and explicit.
    - Against: nothing balances, so a growing site is split by eye. Nothing recovers a lost node until someone re-points its endpoints. The splits drift as polls are added at new intervals.
    - Outcome: kept as the pin (3), not as the mechanism.
  - **Location pools.** A component's pool is its location's nodes, or the nearest ancestor's.
    - For: needs no new entity, since a node already has a location and so does a component. It matches how nodes are deployed (one or more per site).
    - Chosen as the first cut (2).
  - **Cascaded pools** (#1351). A `node-pool` property resolves through the cascade.
    - For: an operator can name a campus pool, exclude a building, or send one system type to a dedicated node, using the same override machinery as everything else.
    - Against: it waits on #1351.
    - Outcome: chosen as the later layer, with the location pool as its default, so no data moves when it lands.
  - **An explicit pool entity** (a `node_pool` table that nodes and locations join). Not chosen: it duplicates the location tree, and the cascade does the same job without a new entity.
  - **Push listeners follow the assignment.** Not chosen: a device would push to one node's address, and every move or lost node would strand it until someone re-pointed the device.
  - **Endpoint as the unit.** It matches #1637's lanes and balances more finely. Not chosen: it splits a device, its sessions and its gate across nodes (1).
  - **System as the unit.** It keeps a room together, so its power, host and path edges stay node-local. Not chosen:
    - a component can be in several systems, so systems do not partition a site;
    - a large system is a lumpy unit;
    - affinity over dependency edges (5) gets the locality without making the system the unit.
  - **Component count as the cost.** Not chosen: a 5 s poll costs six times a 30 s poll.
  - **Measured load as the objective.** Not chosen: moving load changes the measure, so the balance chases its own tail. Measured figures guard instead (4).
  - **A pushed assignment wire.** Not chosen: the pull already carries the worklist, and the 30 s propagation ruling (#1265) holds for a move.
- **Context:** One node at a site has a ceiling; #1624's concurrency work raises it but does not remove it. The corporate shape at 3,000 systems is 30 sites and about 9,000 components, about 300 components a site. A site past one node's ceiling, or one that wants a standby, has no way to use a second node except splitting its endpoints by hand. Co-locating a component with its power, host and path neighbours keeps the gate's hold set and upstream propagation node-local (#1648). A split pair is still correct, carried by the cross-node relay (#1655), so affinity is soft.
- **Measured baseline: not yet measured (thin cut, #1690).**
  - The issue asked for a lab baseline: two nodes at one site, manual placement, and each node's tasks, utilization and max lateness from `omniglass lab measure`.
  - Three things block it on main today:
    - the scale lab runs one node per site;
    - `lab measure` reports no per-node figure;
    - the node publishes neither its lateness (#1625, open) nor a busy fraction (#1686).
  - #1690 adds `--nodes-per-site` to the scale lab with deterministic manual placement and a per-node block in `lab measure`. It records the table here. #1688's end-to-end test is judged against it.
- **Build slices:** #1686 (capacity signal), #1687 (component-on-one-node invariant), #1688 (assignment), #1689 (rebalance on loss and join), #1690 (two-node lab baseline).
