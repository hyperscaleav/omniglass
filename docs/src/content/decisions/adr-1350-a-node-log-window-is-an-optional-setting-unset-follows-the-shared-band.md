---
title: "A node log window is an optional setting; unset follows the shared band"
date: 2026-10-29
status: "Accepted"
summary: "A node's own logs (node_log) may keep each severity band for a window of their own: retention.node_log_debug_days, node_log_info_days, node_log_warning_days and node_log_error_days, install-wide, 1 to 36,500 days. They declare no default, so the effective document leaves them out until an admin sets one, and while one is unset node_log follows the shared band (RetentionSettings.NodeLogDays); log_line always does. This is the first optional setting: no default tag, json omitempty, the zero value read as unset, a merge-patch null restoring it. The Admin settings page lists every key the generated schema declares, so an unset key shows as not set and stays settable. The per-node window waits for the cascade's node leaf (#1351)"
refs: [1350, 1351, 417, 1265, 270]
---

- **Date:** 2026-10-29 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/#retention-what-is-kept-and-for-how-long), [settings](/architecture/settings/), [admin guide](/guides/admin/)
- **Decision:** (#1350 step 1, ruled 2026-10-28: shared bands for now, an install-wide node override first, per node later through the cascade.)
  1. **Four optional settings.** `retention.node_log_<band>_days`, one per band of `logline.RetentionBands`, each held to the range every window is (1 to 36,500; 0 and past the ceiling are a 422). A test holds every band to an override of its own, so a band added later cannot silently ignore one.
  2. **Unset is absence, not a sentinel.** The fields carry no `default` tag and `omitempty`, so they are absent from the effective document and not required in the published schema. The reader (`NodeLogDays`) takes a nonzero value and otherwise the shared band's, so a zero reads as unset. The API refuses a 0 (as it refuses every window below 1), so only the operator file, which is parsed but not validated, can hold one, and there it follows the shared band rather than being refused as a 0 on a shared window is. A stored value above the ceiling or below zero is refused by the pass like any window.
  3. **The console lists declared keys, not only resolved ones.** The Settings card's keys are the union of the effective document's and the generated schema's, so an optional setting renders `not set` and is saved like any other; restoring it returns it to unset. A blank unset row is not an error, and its layer stack marks no level effective and reads the setting's description (now carried in the generated per-field constraint), which names what it follows.
  4. **The pass is unchanged in shape.** `node_log`'s lanes take `NodeLogDays(band)`, `log_line`'s take `LogDays(band)`; same chunking, same indexes, same report.
- **Context:** #417 shared the four band windows between `log_line` and `node_log` to keep the card small. A node's self-logs are platform diagnostics with a different useful life from a component's lines.
- **Not chosen:**
  - **A sentinel default of 0 meaning "follow the shared band".** No console change, but a card reading `node_log_debug_days 0` says "keep nothing", and 0 is the value every other window refuses.
  - **A pointer field (`*int`), so a present 0 is refused.** Huma reflects a pointer as nullable, and `null` is the merge patch's delete sentinel, so the schema would invite a write that means something else (the reason a list setting is `nullable:"false"`).
  - **Per node now.** The cascade has no node leaf; #1351 adds it, and this setting's per-node step is its first consumer.
