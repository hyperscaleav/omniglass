---
title: "A window across a change to the sim's served fleet reads restarted"
date: 2026-11-15
status: "Accepted"
summary: "Each simulator observation carries its fleet generation (zero before the first sync, advanced by each sync that starts or stops a device) and whether such a sync is running. The closed-loop reconcile reads a window whose sim reads hold a zero generation, a changing fleet, or two generations as restarted, the state a counter reset already reads, rather than as loss or a state of its own"
refs: [1895, 1639, 1874]
---

- **Date:** 2026-11-15 | **Status:** Accepted | **Pages:** [simulation](/architecture/simulation/), [platform health](/operator/platform-health/), [lab](/guides/lab/)
- **Decision:** (#1895.) The simulator's engine keeps a fleet generation and a changing flag, and
  each endpoint observation carries both (`fleet_generation`, `fleet_changing`). The reconcile reads
  a bracket as restarted when any sim read in it (its four ends and the reads between) has generation
  zero, is changing, or reads a generation the first does not. The window reads `restarted` until it
  slides past the change.
- **Context:** A lab's first windows span the simulator's first sync. The node polls the devices its
  worklist names as soon as it has one, and a poll to a device the simulator has not bound yet is
  sent and never received, so the browser e2e lab read SNMP loss of about 0.12 on a healthy path
  (#1895). The same holds for a device added or removed later. That is no loss of the path, and a
  threshold on the assertion would hide a real loss of the same size.
- **Consequences:** A healthy lab's first windows read `restarted`, then loss 0. A sync that starts
  or stops a device clears the next window or two. A device that fails to start on every pull marks
  each sync changing for its moment, so a read landing in that moment reads restarted. A simulator
  from a build before this one reports generation zero, so its node's windows always read restarted;
  there is no installed base to mix builds.
- **Not chosen:**
  - **A state of its own (`fleet-changing`).** Every reader of the state (the console, the lab, the
    perf gate) treats it exactly as a restart: no figure compares. A new state would be a new branch
    in each with the same body.
  - **A loss threshold in the e2e assertion.** It hides a real loss below the threshold, and the
    sync's share of loss varies with the fleet's size and the window's length.
  - **Waiting for the first sync before the lab reads.** It covers the first sync only, not a fleet
    change later, and the console would still report the loss it misreads.
