---
title: "An audit row outlives its subject: both images and a string reference"
date: 2026-10-29
status: "Accepted"
summary: "Ruled by the architect on 2026-10-29 for #1339. Deleting or aging out an audited subject never touches audit_log. A row's reference to its subject (resource_id) is a string, never a foreign key, and the before and after JSON images are the record that survives: an update carries each field it moved in both old and new (null where it was unset) and enough of the subject to name it. Every alarm audit row is held to this: acknowledge, clear (by hand and by a rule), snooze, unsnooze and snooze-end each carry the moved field in both images and name the component and dedup key; the raise's new image is the whole alarm"
refs: [1442, 1339, 526, 1318]
---

- **Date:** 2026-10-29 | **Status:** Accepted | **Pages:** [audit](/architecture/audit/#the-model), [alarms and actions](/architecture/alarms-actions/)
- **Decision:** (ruled on #1339, built in #1442.)
  1. **Audit keeps the images.** An update row carries each field it moved in both `old` and `new`, null on the side where the field was unset, and names its subject well enough to read alone (an alarm's component and dedup key). A create carries the whole subject in `new`; a delete the whole subject in `old`.
  2. **A subject reference is a string.** `audit_log.resource_id` is text with no foreign key. Were a reference ever a foreign key, a deletion would have to keep it as text or null it; the images are kept either way.
  3. **Deleting a subject never touches `audit_log`.** Alarm retention (#1339) removes cleared alarms and leaves their audit rows; audit's own retention is #526's.
  4. **Alarms first.** A clear and an acknowledgement wrote no before image, a first snooze wrote none, and an unsnooze or snooze end left the deadline out of its after image; each now carries both, and every row names its condition. A first snooze's before image is `{"snoozed_until": null}` rather than no image, which changes what ADR-1318's tests pinned as "no before".
- **Context:** #1339 asked whether deleting an aged-out alarm would orphan its audit rows. The architect ruled the rows are kept and are meant to stand alone.
- **Not chosen:**
  - **Deleting an alarm's audit rows with it.** Audit is append-only and carries the longest retention.
  - **A foreign key from `audit_log` to its subject.** It would block or rewrite the trail on every deletion.
