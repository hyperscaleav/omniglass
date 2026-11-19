---
title: "Every audit writer carries its before and after (#1444)"
date: 2026-11-14
refs: [1444, 1442, 1339]
---

#1442 made the alarm audit rows read on their own once the alarm is gone. The rest of the writers now
meet the same rule (ADR-1442). Settings, tags, variables, property values, members, role assignments
(assign, swap and unassign), principals (profile, avatar, archive, restore and purge), role choices and
alternates, and webhook tokens each read the row they are about to change inside the writing
transaction, under its lock, and record it beside what they wrote. A delete, including a principal
purge, carries the whole subject in `old`.

Secrets are imaged by metadata and a version digest of the sealed value, never the plaintext; a test
scans every secret and webhook-token row for it. The password verbs carry no field images on purpose,
since the only field they move is a credential hash.

Along the way: a secret's delete image now names its type, a variable's images carry its value and
label, and rotating a webhook token writes the secret's before and after where it wrote nothing.
