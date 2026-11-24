---
title: "A form that cannot be submitted says which field is missing (#2440)"
date: 2026-11-24
refs: [2440, 2432]
---

The system, location and component create faces greyed Create and said nothing. `FormActionBar` now
owns one rule: the primary action stays enabled, and a binding's `problems` turn an attempt (the
button, or Enter in a field) into a sentence beside the actions naming each missing field, a mark on
each, and the focus on the first. `createFormActions` carries the attempt (`trigger`, `attempted`)
so both entry points share it. The event rule form moved from its own sentence above the form onto
the bar. The create faces drop the greyed "Tags: available once created" block, so the action row
is last. `rail-ownership.test.ts` gains a guard that fails a form drawing its own submit button,
with a short allowlist (Login, ForceChangePassword, TimeRangePicker, Omnibot, Profile). Forms that
still bind `disabled` for a missing field keep it until they move to `problems`. Tests:
`EntityForm.test.tsx`, `Rules.test.tsx`, the Systems/Locations/Components create tests.
