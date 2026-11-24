---
title: "An ambiguous name is a 409 listing the matches (#2334)"
date: 2026-11-23
refs: [2334, 1134]
---

Acknowledging an alarm with a component name shared by 30 components returned a bare 404, which
read as "the alarm or the name is wrong". A name that more than one visible row answers to is now a
409 on every collection, listing each match with its id and carrying a machine-readable `path.name`
detail; a name nothing answers to stays a 404. The CLI prints the 409 as "the name matches N rows
you can see; address one by its id or dotted address" followed by the matches. Tests:
`TestAmbiguousNameIs409WithIDsAndUnknownIs404` (API) and
`TestAcknowledgeByAmbiguousNameSaysToUseTheID` (CLI). See ADR-2334.
