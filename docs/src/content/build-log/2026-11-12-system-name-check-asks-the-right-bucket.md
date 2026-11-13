---
title: "System name check asks the right bucket (#852)"
date: 2026-11-12
refs: [852, 627, 834, 1203]
---

The system rename precheck in the web console was passing the system's current location name instead of its location ID to the check endpoint, and passing undefined for the parent when the system has a parent. Both parent and location parameters are the UUIDs that define the name bucket, not the names. The check also silently failed instead of showing an error when the check call rejected.

Fixed by passing the `_id` versions of the placement fields (parent_id, location_id) to the check functions, and displaying the error when a check call fails.
