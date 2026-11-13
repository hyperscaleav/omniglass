---
title: "An ambiguous name is a 404 naming the reference on every collection"
date: 2026-11-13
refs: [1134]
---

Fixed #1134: a component, system or location name that more than one visible row answers to now
answers a 404 like every other collection, not a 409. The message lists each candidate as its dotted
address with its uuid beside it, and either resolves on the same route. A candidate the caller cannot
read is neither listed nor counted. The generated CLI's `<name>` argument help now says a uuid or dotted
address disambiguates a repeated name. See ADR-1134.
