---
title: "The resolution panel reads all three cascades, in one list"
date: 2026-07-22
refs: []
form: bullet
---

- **The resolution panel reads all three cascades, in one list.** It explained the tag cascade only; the
  variable and secret cascades had routes since the effective-values slice and no console read. All three
  are the same engine, so they share one table with a **kind** column rather than tabs or stacked
  sections: their bands genuinely differ (a variable seeds its system band from the **primary**
  membership, a secret has **no** system band at all), and one list makes that visible instead of hiding
  it behind a control. A secret shows its provenance and the word `masked`, never a value: which
  credential a device resolves and from where is the operationally useful fact, and plaintext stays
  behind the audited reveal. The system selector appears only when a tag is in play, since it is the
  only kind that answers per-system. A caller without `secret:read` simply sees the other two kinds.
  Fixed on the way: `bandLabel` still said **global** for the install-wide tier, months after
  [ADR-0057](/architecture/decisions/#adr-0057-the-cascades-least-specific-tier-is-platform-and-a-default-is-not-a-tier)
  renamed it to `platform`. Its test asserted `from global` against a fixture whose `owner_kind` was
  `global`, a value the API stopped producing at that rename, so the label and its test were stale
  together and the suite stayed green.
