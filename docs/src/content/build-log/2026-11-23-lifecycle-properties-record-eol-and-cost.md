---
title: "Lifecycle properties record install, warranty, support, end of life and cost (#2366)"
date: 2026-11-23
refs: [2366, 528]
---

Six official lifecycle fields are seeded: `install-date`, `warranty-end`, `end-of-support` and
`end-of-life` (string types with an ISO-8601 date pattern) and the `replacement-cost` and
`expected-life-years` metric types. A product default is a component's effective value and a component
value overrides it. Operator-declared property values are now held to their type's data type and
validation (a malformed date is a 422), which they were not before. The component and product pages
show a formatted Lifecycle group.
