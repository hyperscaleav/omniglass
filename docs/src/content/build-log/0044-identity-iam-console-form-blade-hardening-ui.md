---
title: "Identity: IAM console form + blade hardening, UI"
date: 2026-07-09
refs: []
form: bullet
---

- **Identity: IAM console form + blade hardening, UI.** A polish pass over the identity surfaces. Removing a
  principal now closes its blade cleanly: **archiving**, **purging** (and a group **delete**) closes the blade
  first, drops the dead detail query, then refreshes the list, so the blade never lingers on a stale or 404 view
  of the entity it just removed from the working set.
  **Impersonation** (start and stop) now reboots the console to Home (`window.location`), so the
  permission-driven sidebar and route guard rebuild from the new identity rather than showing the previous
  principal's navigation. The forms gain **inline validation** that mirrors the server's Huma constraints: a
  username and a group name are lowercase handles (pattern `^[a-z0-9][a-z0-9._-]*$`, no capitals or spaces) and
  an email must be well formed (`format: email`), each enforced on the create body server-side and checked in
  the form so an invalid field shows an inline error and disables the submit before the round-trip (a new
  `valid` gate on the blade edit contract disables the footer **Save**). The group create Drawer title now reads
  **New group** (it leaked the `principal_group` resource key), the group create form gained the same
  subtitle the user form has, and the form placeholders are consistent across create and edit. A newly created
  **user or group** opens **directly in edit mode** so its grants (and a group's members, child resources that
  need the parent id) are added in one flow rather than a second step. Proven by an API
  validation test (a malformed username, name, or email is a 422; a valid one is accepted) and web tests (the
  blade closes without an orphan refetch, and an invalid field blocks create and the footer Save).
