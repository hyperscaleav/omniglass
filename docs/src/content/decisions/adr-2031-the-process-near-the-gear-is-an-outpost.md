---
title: "The process near the gear is an `outpost`"
date: 2026-11-16
status: "Accepted"
summary: "The edge process that polls, listens, carries commands and holds sessions near the gear is an outpost on every surface. Node retires as its name; edge and central stay as placement values, not part of the noun"
refs: [2031, 2028, 2059]
---

- **Date:** 2026-11-16 | **Status:** Accepted | **Pages:** [glossary](/architecture/glossary/),
  [nodes](/architecture/nodes/)
- **Decision:** (#2031.) The process that runs near the gear on the server's behalf is an
  **outpost**, on every surface: the run mode (`omniglass outpost run`), the API (`/outposts`, the
  `outpost:*` permissions), the schema (`outpost`, `outpost_log`, an endpoint's `outpost_name`), the
  CLI group, the console's Outposts page and the docs. **Node** retires as its name and joins the
  docs denylist, with an Allow for a tree or graph node. Placement keeps its two values, **edge**
  and **central**: edge work runs on an outpost, central work runs on the server, so the server
  never has to be described as a node of its own. Edge is an adjective in prose, never part of
  the noun. Go code keeps worker for goroutine pools. The renames are #2059.
- **Context:** node says nothing about the job and collides with the tree and graph nodes the
  outline and topology already draw, and the target placement design had to call the server
  "itself the node target" for central work. An outpost does the server's work near the gear:
  it polls, listens for pushes, carries commands, holds sessions and buffers, and a site may have
  several. The word covers every one of those jobs, appears nowhere else in the tree, and names
  the relationship to the server rather than a distance. Precedent: authentik's outposts are
  remote deployments that do the core's work near the resource. Considered: **collector**,
  self-describing and familiar from monitoring, but it undersells commands and sessions;
  **worker**, job-agnostic, but already the pool-unit word (lane workers, the server's worker
  stages) and generic, since the server works too; **proxy**, the Zabbix word, which collides with
  the trusted reverse proxy and with network proxies that are components in the fleet, and reads
  as pass-through; **satellite**, the Icinga word, which is also a Red Hat product; and an **edge**
  qualifier on any of them, which head-noun-last naming leaves as a kind of the same genus (an edge
  worker is still a worker beside the lane workers), and which is wrong for central placement.
