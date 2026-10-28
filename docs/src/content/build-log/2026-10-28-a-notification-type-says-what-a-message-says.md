---
title: "A notification type says what a message about an alarm says (#1319)"
date: 2026-10-28
refs: [618, 1319]
---

The first slice of the notification lane (#618). A `notification_type` is a registry row with two
templates, a one-line subject and a body text, the outbound peer of `command_type`. Two official
types ship, `alarm-raised` and `alarm-cleared`, through the authoritative boot upsert; an operator
creates, patches, renames and deletes their own (`notification_type:create,update` on operator,
`notification_type:*` on admin, read on the viewer floor, no scope injected).

The templates render through the label engine's sandbox, lifted into `internal/sandbox` so a label
rule and a notification are one primitive rather than two. A label keeps its 200-character cap and
whitespace collapse in `internal/label`; a notification's subject collapses too, its text keeps its
lines, capped at 4000. The alarm facts a template reads are declared once in `internal/storage`
beside the accessor that builds them and rendered into the docs by `cmd/labelgen`, the same way the
label data map is.

`POST /notification-types/{name}:preview` renders a type, or an unsaved draft of it, against a
component's real alarm and returns the facts beside the result, writing nothing. The console's
Notifications catalog page stops being a soon stub: it lists the types, edits an operator's
templates, and previews them through that route.
