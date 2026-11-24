---
title: "A list's sort and row open work from the keyboard (#1932)"
date: 2026-11-23
refs: [1932]
---

On every `FlatList` page the sort header was a clickable `<th>` and the row a clickable `<tr>`,
so nothing in the table could take focus: no keyboard sort, no keyboard open, and axe's
`scrollable-region-focusable` fired on an overflowing table. A sortable header now renders its
label in a `<button>` and the `<th>` reports `aria-sort`; an openable row ends in a button named
`Open <name>` (new optional `rowLabel`, defaulting to the entity noun and the row's address, so rows differ). Both come from the
primitive, so all 27 consumers inherit it. Tests: `FlatList keyboard operation` in
`FlatList.test.tsx`.
