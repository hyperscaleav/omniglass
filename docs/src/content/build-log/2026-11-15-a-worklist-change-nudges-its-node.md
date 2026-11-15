---
title: "A worklist change nudges its node to pull at once (#1945)"
date: 2026-11-15
refs: [1945, 1997]
---

A node pulled its worklist only on its own interval, 30 s jittered, so an operator's edit waited up to
about 33 s to run. Now the commit of an endpoint create, edit, retry or delete, and a driver spec change
that re-derives an endpoint's tasks, issues a Postgres `NOTIFY` naming each node it touched; the server's
one listening connection forwards each name as an empty message on `og.v1.worklist-changed.<node>`, and
the node pulls at once, at most once every 200 ms, with the scheduled pull kept as the backstop for a lost
nudge. `TestAWorklistChangeReachesTheNodeAtOnce` runs a node whose scheduled pull is an hour: a created
endpoint runs within a second and an address edit moves within a second (with the node's handler emptied,
the create waits out the second and fails), and 100 edits over 0.8 s cost 3 pulls.
`TestWorklistChangesAnnounceTheNodesAWriteTouches` checks each write names exactly its nodes (both, on a
move), a refused edit names none, and the listener comes back after its connection is killed. The other
writers that change a worklist (secrets, property pins, dependencies, the report cadence) are #1997.
The decision, a notify on commit rather than the outbox, is ADR-1945.
