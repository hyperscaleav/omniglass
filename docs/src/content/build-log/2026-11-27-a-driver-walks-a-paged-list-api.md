---
title: "A driver walks a paged list API and fans every page's records out (#2789)"
date: 2026-11-27
refs: [2789, 2757]
---

A driver's poll request may declare `paginate`, and one poll walks every page of the list
([ADR-2789](/architecture/decisions/#adr-2789-a-poll-declares-how-its-list-pages-and-the-engine-walks-every-page-in-one-poll)):
a cursor read from the body or a header and sent back as a query parameter or header, or a page number
with an optional declared total. The records of every page are gathered, then identified and fanned
out once, so a 3,000-room account is one collection. The engine names no field: the driver does. Bounds
(`max_pages`, `max_records`) and a failed page fault the poll naming the endpoint and land nothing.
`ucc-rooms` declares `next_page_token` as its cursor, and the simulated UCC platform pages
`GET /api/rooms` by `page_size` and `next_page_token` (the record shape is unchanged).
