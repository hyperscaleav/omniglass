---
title: "Bookings are exclusive-arc-owned schedules, reconciled against observed usage"
date: 2026-07-28
status: "Accepted"
summary: "Bookings are exclusive-arc-owned schedules, reconciled against observed usage"
---

- **Date:** 2026-07-28 | **Status:** Accepted

Tracking how systems and spaces are used needs the **scheduled** side, not only the observed telemetry.
A booking (a room reservation, an equipment checkout, a virtual-room reservation) is what a calendar
system holds, and it is a different shape from a telemetry event: an **interval** (a start and an end)
with a lifecycle (created, moved, cancelled), and it is **declarative**, someone reserved something.

**A `booking` is its own entity, owned through the exclusive arc.** It uses the same `owner_kind` plus
one-of (`component_id` / `system_id` / `location_id` / `node_id`) plus the CHECK that exactly one is set,
the ownership model the rest of the estate uses. The arc **is** the binding, so no separate resource
layer is needed: a room booking owns to a **location**, a mobile-equipment checkout owns to a
**component**, a virtual meeting room owns to a **system** (the bridge modeled as a virtual conferencing
system), and a bookable space with no AV owns to a **location** that has no systems. The heterogeneity
flagged during design review (not every reservation is a physical room) is exactly what the arc
already expresses.

**A booking is an observed intended schedule.** Its provenance is **observed**: the platform did not
declare it, it caught it from an external calendar. Its meaning is **intended**: a declaration of how the
owner is meant to be used. A booking therefore carries observed provenance of intended use, and it is the
**intended-schedule** side of usage. Shape: `booking { id, owner arc, source, external_id, subject,
organizer, start, end, status, attendee_count, ... }`, deduplicated and updated on `(source, external_id)`.

**Sourcing is a calendar driver on one platform-level integration, not a per-room mailbox fanout.** A
per-room-mailbox model fails operationally at scale (one subscription and one credential per room), and a
calendar is a **source**, not a device. Instead there is one platform-scoped integration component per
calendar tenant, holding the tenant application credential, exposing a `calendar` **interface whose
`interface_type` is a driver** (`graph-calendar`, `google-calendar`), consistent with the
interface-is-a-driver model. A server-side worker runs the driver, enumerates the tenant's bookable
resources, and fans out **internally**, upserting each booking with its owner bound through the arc. The
operator's only seam is mapping a resource to its owner (auto-matched by resource address, operator-confirmed).

**Bookings drive usage reconciliation and utilization SLIs.** The booking is the intended schedule;
telemetry (`call.started`, occupancy, an active input) is the observed actual. Reconciled per owner and
window: booked and observed is real utilization, booked and not observed is a no-show, observed and not
booked is ad-hoc use. These are **SLI slices** in the health layer (utilization percent, no-show rate,
ad-hoc rate, per location / system / component / window). An owner with no observability (a booked space
with no systems) degrades gracefully to a booked-only SLI.

**Deferred, each its own slice.** The resource-to-owner binding (auto-match versus operator-set);
**privacy**, since booking subjects and organizers are more sensitive than device telemetry, so store the
minimal busy/free plus booked-by by default and make the full subject opt-in; the platform order
(Microsoft 365 Graph first, Google Workspace second, Teams and Zoom Rooms scheduling as a later layer);
and recurrence (expand a series to instances versus store the series) with the sync window. Nothing here
is built. This ADR records the target so the booking slice ([#412](https://github.com/hyperscaleav/omniglass/issues/412)) inherits a decided model.
