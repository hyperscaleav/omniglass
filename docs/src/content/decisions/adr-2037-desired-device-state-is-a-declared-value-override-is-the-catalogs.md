---
title: "Desired device state is a declared value; override is the catalog's"
date: 2026-11-16
status: "Accepted"
summary: "What an operator wants a device to be is a declared value, the word the schema already uses, paired with observed; config stops naming it. Override means only an operator's sparse edit to a shipped catalog row: a value set at one cascade level is a binding (setting_override becomes setting_binding) and a deeper binding shadows a shallower one. Variable, secret, setting, tag, property and default keep their meanings"
refs: [2037, 2028, 1435, 2079]
---

- **Date:** 2026-11-16 | **Status:** Accepted | **Pages:** [glossary](/architecture/glossary/),
  [config, secrets, and variables](/architecture/variables/), [settings](/architecture/settings/)
- **Decision:** (#2037.) One word per kind of value.
  - **Declared value** is desired device state: what an operator asserts a device should be, stored as
    a property row with `provenance = 'declared'`
    ([ADR-0079](/architecture/decisions/#adr-0079-five-telemetry-lanes-and-property-stops-being-the-genus)).
    It pairs with **observed**, and the gap between the two on one signal is **drift**. **Config**
    stops naming it in the console and docs: the planned Values entry becomes **Declared values**,
    the page "Config, secrets, and variables" retitles, and "declared config", "desired state" and
    "desired value" join the denylist. Config keeps one sense, the server's own startup
    configuration (flags, environment, file). **Intended** stays a command's expected effect, a
    separate provenance; **want** stays the console's column header over the declared value.
  - **Override** means only an operator's sparse edit to a shipped (`og`) catalog row, the
    `override` column #1435 adds. A value set at one level of a cascade is a **binding**, as
    `tag_binding` and the glossary's cascade row already say: `setting_override` becomes
    `setting_binding` in a new migration, with its constraints and the settings gateway. In
    cascade prose a deeper binding **shadows** a shallower one; it does not override it.
  - Confirmed with no rename, each already one meaning on every surface: **variable** (an
    inherited interpolated value, `$var:`), **secret** (an inherited encrypted value),
    **setting** (a platform or user preference, `/settings`), **tag** (an operator `key: value`,
    a governed key with values bound per entity; **label** stays the friendly display string of
    [ADR-0118](/architecture/decisions/#adr-0118-the-friendly-string-an-operator-reads-is-a-label-and-unset-is-sql-null)),
    **property** (the categorical signal: a property type is its catalog row and
    `product_property` and its siblings are contract lines declaring one, one concept seen from
    three sides as metric is), and **default** (what a value is when nobody bound it).
  - The renames are #2079.
- **Context:** the console's Values nav reads Variables, Secrets, Config, Files, with Settings
  under Admin, and the docs page that teaches them opens "config: declared device state". Counted
  across the API, schema, permissions, CLI, console and docs, config named desired device state
  only in prose: no table, route, permission or CLI command carries it, the Config entry is not
  built, and in Go config is overwhelmingly the server's own configuration. The code says
  declared (the provenance value, 709 Go hits for the word) and the docs say "declared value"
  86 times, against 3 for "declared config" and 3 for desired. Override had three senses: the
  `setting_override` table (46 hits in 21 files); the cascade itself, in prose and generated help
  alike (the cascade page's "set once, override where it matters", the tag and variable
  combinators' "values override most-specific-wins", a component property's `set_value` described
  as "the component's override"); and #1435's catalog override. The glossary's default row already
  said shadows for the cascade, and the tag combinator becomes union on key, shadow on value.
  Considered and rejected: keeping config as the operator noun (the AV word, but code and
  console would then say two words for one row, what
  [ADR-2035](/architecture/decisions/#adr-2035-the-catalog-is-what-the-fleet-is-typed-by-a-registry-is-compiled-in)
  rejected); desired, the Kubernetes pairing, which would rename the provenance value and every
  declared-value page for no gain in meaning; and keeping override in both senses with #1435's
  column renamed (a patch), which leaves the cascade and the catalog edit sharing a word an
  operator meets on the same Settings and catalog pages.
