---
title: "The server's label order is the order the console renders (#757)"
date: 2026-11-12
refs: [757, 613, 1206]
---

The rule "a row with no label sorts last, otherwise by label, then by name" lived in two languages: SQL
(`order by label nulls last, name` on every registry list) and `byLabel` in the console, which re-sorted
every list the server had already ordered. A screenshot caught the one time they disagreed; no test did.

The console now renders a served list in the order it arrives. An audit of every `byLabel` call site
removed it from the registry pages (tags, secrets, variables, products, vendors, drivers, standards,
location types), the pickers over those registries, the role editor's catalogs, and the system and
component type trees, which now group without sorting. Three sites keep it, because their endpoints
serve another order (secret types and nodes by name, the template catalog by kind then name); each says
so, and `byLabel`'s doc comment lists them.

One fixture, `web/src/lib/label_order.fixture.json`, holds the rows and the sequence the rule must
produce. A storage test creates the rows and reads them back through `ListTags`; a web test sorts them
with `byLabel`; both assert the same committed sequence, so changing the rule on one side fails the
other. `FlatList` is pinned to render rows as received until the operator sorts a column, and the Tags
page to render the server's order without re-sorting. The design system states the rule: lists render in
server order, and the SQL rule is the ordering primitive.
