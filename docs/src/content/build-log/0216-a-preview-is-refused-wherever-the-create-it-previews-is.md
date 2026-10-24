---
title: "A preview is refused wherever the create it previews is refused"
date: 2026-08-13
refs: []
form: bullet
---

- **A preview is refused wherever the create it previews is refused.** `POST /components:renderLabel`
  resolved its `system` reference in `system:read` while the create beside it had moved to
  `system:update`, so a caller holding the read floor and no membership authority was served a
  drafted label for a create the platform then refused, and the label it was served carried that
  system's own type name. Nothing hit it in practice, because the console does not offer the picker
  to a principal that cannot use it, which is the agreement being kept by the caller's good behaviour
  rather than by the platform. The draft now resolves that reference through the create's own
  resolver, in `system:read` and `system:update`, and carries the create's conditional
  `system:update` permission, so both halves of the gate rehearse and both routes answer the same
  sentinel: 403 naming the authority for a readable system, the non-disclosing 422 for one the caller
  cannot see. The LOCATION reference on the same two routes was checked in the same pass and is
  already aligned at `location:read`, deliberately, because a location is read and rendered into the
  label rather than written
  ([ADR-0107](/architecture/decisions/#adr-0107-a-create-that-writes-a-membership-costs-what-the-membership-route-costs),
  [ADR-0104](/architecture/decisions/#adr-0104-a-create-form-shows-the-name-it-can-know-and-never-mints-one-to-preview-it)).
