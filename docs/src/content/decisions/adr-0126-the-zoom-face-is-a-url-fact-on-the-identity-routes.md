---
title: "The zoom face is a URL fact on the identity routes"
date: 2026-08-15
status: "Accepted"
summary: "The deeper zooms render at the identity routes behind a query param (`/locations/{id}?zoom=1`), the inventory detail staying the default face: ADR-0120's mode-rides-the-URL precedent applied to the fork where \"the zoom is a function of which entity the URL names\" and \"the four tables stay live and untouched\" collided at one address. The param may become the default later, and the table face may retire once the medium is judged"
---

- **Date:** 2026-08-15 | **Status:** Accepted | **Pages:** [ui](/architecture/ui/)
- **Decision:** the location, system and component zooms render at the identity routes the epic
  already ruled (`/locations/{id}`, `/systems/{id}`, `/components/{id}`) behind a query param
  (`?zoom=1`), with the inventory detail remaining each route's default face. A fleet-canvas
  drill carries the param; deep links, refresh and the back button behave as ADR-0120
  established for the edit face, which is this decision's precedent verbatim.
- **Context:** the epic ruled both "the zoom is a function of which entity the URL names" and
  "the four tables stay live and untouched while the medium is explored", and the two collided
  at one address: the inventory detail owns `/locations/{id}` today. The architect's ruling
  (2026-08-15): the zoom was originally intended to replace the old views, and the param is the
  right first step; it may become the default later, with the table face retiring entirely once
  the medium is judged. Rejected: a location param under `/fleet` (a second address for the same
  row, which the epic forbids) and replacing the detail now (falsifies the tables-stay ruling
  mid-exploration).
- **Tracked under** epic [#630](https://github.com/hyperscaleav/omniglass/issues/630), ahead of
  slice [#635](https://github.com/hyperscaleav/omniglass/issues/635).
