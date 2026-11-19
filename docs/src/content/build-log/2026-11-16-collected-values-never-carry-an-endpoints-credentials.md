---
title: "Collected values never carry an endpoint's credentials"
date: 2026-11-16
refs: []
---

A node already scrubbed a component's text of the credential before it became a rejection reason. Now the same holds for everything a task collects. The worklist names each task's mask, which is the values of the fields its secrets' types mark secret. The node masks those values, along with the base64 form a basic authorization takes on the wire, out of every string in a collected group before the group is queued. That covers metric labels and identities, property values, event messages and payloads, and log lines with their attributes. JSON payloads are masked one string at a time, so they stay valid JSON. Masking happens at the telemetry spool, which every lane passes through, and each task's mask is replaced on every worklist. The grouped worklist wire carries the masks in their own dictionary, so tasks that share a credential also share one mask entry.
