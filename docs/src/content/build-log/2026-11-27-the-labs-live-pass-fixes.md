---
title: "The live pass of the lab calendar: four fixes on a corporate lab (#2757)"
date: 2026-11-27
refs: [2757]
---

A live pass over a corporate lab found four things the tests had not. The meeting platform's rooms
list no longer holds its devices: the simulator was handed every value bound on the platform's
endpoint as a room, device ids included, so `dev-<hash>` ids waited in the unbound inbox as room ids;
only values bound to a system are room ids now. A slot of several components reads with its ordinal
("Display 1", "Display 2") because the world's names carry none the platform owns, and the
account-wide services read as the manifest labels them ("Meeting Platform") rather than as their
component type. A conference room's `occupancy-count` now follows its people counter: the counter's
raw-TCP band (`tessellate-census-feed`) reports `people-count` on the component, the one thing the
http API never carried, and the `room-occupancy` calc rule seeded with the plan writes the most any
counter reports as the room's own. The control band's line ending is now CRLF so the driver's line
session can read it. A room's temperature still has no device in the lab to read it from.
