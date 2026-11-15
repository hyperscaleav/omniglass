---
title: "Every worklist writer nudges its node (#1997)"
date: 2026-11-15
refs: [1997, 1945]
---

The change nudge (#1945) covered the endpoint's own writes; a rotated secret, a dependency edge, a
renamed upstream, a moved component or location, and the report window still waited for the node's
scheduled pull. Now each of those writes announces, in its own transaction, exactly the nodes whose
worklist it changed: a secret's create, field edit or delete names the nodes of the endpoints that
resolve it; a path edge names the nodes holding both ends in band; a rename names the nodes carrying the
component as an upstream; a reparent or relocation names the endpoints below it; a settings write that
moves `collection.report_seconds` names every node. A label-only secret edit, an unchanged setting and a
refused write name none. `TestARotatedSecretReachesTheNodeAtOnce` runs a real node pulling hourly: the
device sees the rotated api key within a second (against the old secret writer, it never does). The
storage tests in `worklist_writers_test.go` check each writer names exactly its nodes. Key pins needed
nothing new: the plain property route refuses them, and an endpoint's `:retry`, their one writer,
already nudged.
