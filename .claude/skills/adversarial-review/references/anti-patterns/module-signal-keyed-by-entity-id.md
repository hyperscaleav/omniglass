## module-signal-keyed-by-entity-id (fixed; found by the #1383 review)

- **Cue:** UI state (an open menu, an error) hoisted out of a polled list's rows into a
  module-level signal keyed by an entity id alone, so it survives the rows being rebuilt.
- **Failure:** one alarm is on screen twice at once (the component blade's Why strip and its
  alarms panel); clicking Snooze in either set the shared key, both Kobalte popovers opened,
  each took focus, and each one's focus-outside dismissed the other, so the menu never stayed
  open and the blade could not snooze at all.
- **Fix:** key the state by a list-owned surface id (`createUniqueId()` in the list's owner,
  which outlives its rows) plus the entity id (`SnoozeControl` in
  web/src/components/SnoozeButton.tsx), or keep rows stable by id and hold the state in the row.
