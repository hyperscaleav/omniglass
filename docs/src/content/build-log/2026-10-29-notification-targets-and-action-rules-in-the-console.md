---
title: "Notification targets and action rules have console pages (#1346)"
date: 2026-10-29
refs: [1346, 618, 1341]
---

The admin half of the notification lane moved from the API and CLI into the console. **Catalog, under
Actions** now holds **Action rules** and **Targets** beside **Notifications**, each a `FlatList` over its
registry with a blade, a create Drawer and an edit Drawer, the Rules page's shape.

A target's panel says where it sends for every kind (a webhook's URL masked to scheme and host, a
mailbox's addresses, a group or a person with the channels it tells on), reveals a webhook's URL whole
with one audited `:reveal` per click, and sends a test to a webhook or a mailbox, showing what the
receiver answered or why it did not take the message. It names the action rules that send to the target
(the reason a delete is refused) and lists what was recently sent to it. Targets is admin-only like the
routes behind it: `notification_target` is a sensitive resource, so its subrail entry is off the read
floor.

An action rule's form writes its `when` in the console's one expression editor at the action site, checked
as it is typed and refusing a save until it compiles; a refusal the server places on `when` lands beside
the editor. Its panel carries the Disable and Enable switch and the rule's recent notifications, picked
from the newest 500 the caller may see, since `GET /notifications` filters by target but not by rule. A
person's own subscription can share a platform rule's name, and its rows travel through no target, so
they are left out.
