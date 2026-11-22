---
title: "The Drivers page joins the Admin section (#2200)"
date: 2026-11-22
refs: [2200, 2190, 2159]
---

The Drivers guide was 579 lines, about 460 of them the declarative spec reference. It is now two
pages: `admin/drivers.md` covers the catalog (official rows read-only, admin-gated custom ones),
attaching a driver to an endpoint, built-in and collected data, records and identity, and checking,
previewing and running a poll now; `developer/driver-spec.md` carries the spec reference for driver
authors. `guides/admin/drivers.md` is deleted and redirects to the Admin page, and every link to it
(including the `#built-in-and-collected-data` anchor) now points at `/admin/drivers/`. The empty
Admin guide group left the sidebar.
