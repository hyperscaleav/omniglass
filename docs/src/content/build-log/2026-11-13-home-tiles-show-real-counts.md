---
title: "Home tiles show real counts (#983)"
date: 2026-11-13
refs: [983, 523]
---

Home's Open alarms and Systems tiles were a permanent dash under "collection pending", which told an
operator with a running fleet that nothing was being collected. Systems now counts the fleet view's
systems, Collectors (new, shown to a caller who can read nodes) counts the node list, and each opens its
list page. Open alarms reads a new `open_alarms` on `GET /views/fleet`: the alarms open on the components
the caller may read, counted in one statement with the component scope injected, and absent rather than
zero for a caller with no component read. A tile that cannot count reads an error or the permission it
needs. The Open alarms tile is not a link yet, because `/alarms` is still a stub page. The situation-room
redesign stays with #523.
