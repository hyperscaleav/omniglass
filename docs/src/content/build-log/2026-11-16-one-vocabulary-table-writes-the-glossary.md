---
title: "One vocabulary table writes the glossary and the API descriptions (#2029)"
date: 2026-11-16
refs: [2029, 2028]
---

Every word an operator or an admin reads is now a row in `internal/vocab/terms.yaml`. `make gen` renders the new [glossary](/reference/glossary/) from it, and each mapped OpenAPI entity schema carries its term's definition as its description. Guards fail any API collection, console nav entry or health verdict that names no term, a nav label that is not its term's word, and a retired word in console strings or docs prose. Retired words now live in the table beside the word that replaced them. The nav hints and docs prose no longer say workspace or inventory, and the architecture glossary is marked as the frozen internal reference.
