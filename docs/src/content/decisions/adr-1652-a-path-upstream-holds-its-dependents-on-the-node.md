---
title: "A path upstream holds its dependents on the node"
date: 2026-11-10
status: Accepted
summary: "Accepted: a path dependency edge is followed live on the node. While any endpoint of a component's path upstream has a closed gate, for any reason, the dependent's in-band endpoints hold an upstream reason naming the root of the closure: polls held and counted, probes not run, commands answered at once. Once the upstream opens the reason waits for the dependent's own probe to read up. One pure function derives it, transitive and cycle-guarded, each time any reason set moves."
refs: [1652, 1650, 1648, 1651, 1655, 1624, 1703]
---

- **Date:** 2026-11-10 | **Status:** Accepted | **Pages:** [nodes](/architecture/nodes/#implicit-reachability), [collection](/architecture/collection/#the-connection-fingerprint-and-the-layered-gate), [core entities](/architecture/core-entities/#dependency-edges-what-a-component-needs-apart-from-where-it-sits)
- **Decision:**
  1. **The worklist carries the edges.** A task reached through an upstream carries its component
     and its path upstreams: for every `path` edge from the task's component, the upstream's id,
     name and in-band endpoints placed on the same node. An upstream's own component rides the edge
     that names it, so the tasks reached through nothing (most of a large worklist) pay no bytes for
     it and an 800-component site stays under the worklist's wire cap. An out-of-band task carries
     none, so a management port is
     never held by what its device is reached through; an upstream on another node is left out
     (#1655); `power` and `host` edges are command-triggered holds (ADR-1648, ADR-1651), not this.
  2. **Node-local and live.** The node derives every endpoint's `upstream` reason with one pure
     function (`deriveUpstream`) each time any endpoint's reason set moves (a verdict, a refusal, a
     worklist adoption), so a dependent closes in the same pass as its upstream, not on the next
     pull. The moved endpoints are published at once and the dispatcher re-reads them.
  3. **Any closed gate holds, refused included.** Narrowing which reasons hold (say, not an http
     auth refusal) waits for the lab to show over-holding.
  4. **Held, then waiting.** While the upstream is closed the dependent's polls are held and counted
     as `poll-held-count`, its probes do not run (so its probe result never moves and nothing fires on
     it: one root cause, no storm), and a command, run now or preview answers at once. When every
     upstream opens the reason moves to its wait phase: probes resume, polls stay held, and the
     dependent's own up verdict releases it. A dependent with no probe is released at once; one that
     earns its own `unreachable` while waiting drops `upstream`, so its gate names its own cause.
     Downs its probe read before the hold do not count toward that `unreachable`: they saw the same
     outage through the upstream.
  5. **The reason names the root.** A dependent of an upstream closed only by a held-phase `upstream`
     reason inherits that reason's component; any other closure names the upstream itself. Down A to
     B to C, both B and C read `upstream A` while A is closed; once A opens and B waits on its probe,
     C reads `upstream B`. Among several closed upstreams the one already named is kept, so the
     answer does not flap. Upstream ranks first among the kinds: it is the cause the others follow.
  6. **Cycle-guarded.** No reason is derived from a closure rooted in the endpoint's own component,
     and a waiting upstream never holds a component it depends on itself, so a path cycle terminates
     (the iteration reaches a fixed point) and never holds itself up.
  7. **Published and adopted.** The reason rides the endpoint's rejection row as `upstream <since>
     <component-id>`; the reachability read serves the component's current name beside the id when
     the caller may read it. A restarted node adopts it as the prior hold, keeps it held until the
     upstream's probes read up (the upstream's own `unreachable` is re-earned, not adopted), and then
     re-derives it live, so a restart never drops a hold unseen, never invents one, and never probes
     through a dead upstream.
- **Divergences and follow-ups:** a held session's own reconnect is not gated by `upstream` yet
  (#1703); an upstream or dependent on another node is #1655; the console rendering of gate reasons
  is #1657.
- **Rejected:** probing dependents while held (each would read down and raise its own alarm behind
  the one gateway, the storm the edge exists to prevent); releasing a dependent the moment its
  upstream opens (a peripheral that has not re-registered would read as fresh loss); deriving the
  reason on the server and shipping it on the next worklist (a pull is up to 30 s late, and the
  probe that closed the upstream is already on the node); naming the immediate upstream rather than
  the root (an operator would chase B while A is the cause).
