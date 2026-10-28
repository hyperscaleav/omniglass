---
title: "A standard forks from a template, and the stand-in seed retires (#1324)"
date: 2026-10-28
refs: [1324, 827]
---

`POST /standards:fork` (`omniglass standard fork --template <template>`) writes one standard of the
caller's own from a standard template: its choices, roles, property and metric contract lines and map,
in one transaction, gated by `standard:create` and audited once as `fork` with the template's name in
the detail. A taken name is a 409, a variant whose parent the caller does not hold is a 422 naming the
template to fork first, and an unknown template is a 404. The boot seed stops planting standards, so a
fresh install holds none; a migration drops the `standard.seeded` mark, and with it the rename's refusal
of a seeded example, so a standard an older release planted renames and deletes like any operator row
and never comes back. The dev seed and the lab fork every standard template through the same primitive
before they build their fleet, which also gives them the metric contract and layouts they used to add
by hand. The catalog's build checks grew with the review: a system template must build to a standard
template the catalog ships, a preset's values and a declared value are held to the checks the attach
and the write apply, a fill's product must be a type its role accepts, a map is held to the write's own
rule, a second YAML document is refused, and a schema test pins that no table or column is a template
([ADR-0827](/architecture/decisions/#adr-0827-the-template-layer-owns-the-clone-model-and-the-componenttemplate-authoring-model-is-superseded-by-the-driver-spec)).
