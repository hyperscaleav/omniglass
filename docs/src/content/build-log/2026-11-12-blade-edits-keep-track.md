---
title: "Blade and workspace edits keep track of the edit (#854)"
date: 2026-11-12
refs: [854]
---

Four gaps in the blade and Configure edit flows closed. Deleting a component from its blade now also
refreshes the system roles and health reads, so a system page no longer shows the deleted component
staffing a role. A failed Save shows its reason in the footer beside Save (a new `SaveError`, fed by the
edit slot) instead of only at the top of the form, and the blade footer no longer swallows the
rejection. Leaving Configure with unsaved drafts asks first (`lib/leave_guard.ts`, asked by the tab
rail), a component's staged membership changes counted among the drafts: confirm discards, cancel
stays. The Configure face is keyed on the route id in the system,
location and component pages, so moving to another entity starts a fresh edit. A component that staffs
a role is still refused by the server with a 409, and the blade says so. Panel drafts (fields,
tags) are not part of the dirty check yet.
