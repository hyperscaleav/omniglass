---
title: "An ambiguous name is a 409 listing the matches, a missing one stays a 404"
date: 2026-11-23
status: "Accepted"
summary: "A name that more than one visible row answers to is a 409 Conflict on every collection (#2334), reversing the 404 ADR-1134 chose: the body lists each match with its id and carries a path.name error detail the CLI keys on to say the name matches N rows and to use the id. A name that matches nothing is still a 404"
refs: [2334, 1134, 2269]
---

- **Date:** 2026-11-23 | **Status:** Accepted | **Pages:** [CLI](/developer/cli/)
- **Decision:** (#2334, reversing item 1 of ADR-1134.)
  1. **Ambiguous is 409, absent is 404.** A bare name that two or more rows the caller can see answer
     to is a 409; a name no visible row answers to is a 404. An operator copying a name from a list
     (30 components named `videobar-1`) read the 404 as "the alarm or the name is wrong".
  2. **Machine-readable.** The 409 carries an error detail at location `path.name` whose value is the
     count and the listed candidates (at most ten, each a dotted address with its uuid). The CLI keys
     on it to print "the name matches N rows you can see; address one by its id or dotted address".
  3. Candidate spelling, redaction and the no-caller-scope wording are unchanged from ADR-1134.
- **Context:** ADR-1134 argued a 409 reads as a conflict with a stored row. The night-shift report
  showed the opposite failure: a 404 reads as nonexistence. The status now says the request is
  unresolvable as stated, and the body says how to fix it.
