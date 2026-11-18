---
title: "The Admin section: overview, access and values"
date: 2026-11-18
refs: [2158, 2153]
---

The Admin section's first half lands under `/admin/`: an overview that says what makes someone an
admin and where each surface lives, then users, roles and grants, the audit trail, secrets,
variables and files, each badged. Secret types folded into the secrets page, since a type only
matters as the shape of a secret. A new **Platform settings** page holds what the old overview
carried (how long data is kept, the report window) and names where the other namespaces are
taught; its retention examples use the generated `omniglass setting update`, now that it takes a
body. Custom roles and the retention of events, alarms and the audit log are fenced as design. The
old URLs redirect. The catalog and automation pages stay under the Admin guide until #2190 and
#2191.
