---
title: "A poll declares how its list pages, and the engine walks every page in one poll"
date: 2026-11-27
status: Accepted
summary: "An http poll's request may declare `paginate`: a style (`cursor`, a value read from each answer and sent on the next request, or `page`, a counter), where the cursor is read and sent (every field and parameter name is the driver's, none is the engine's), an optional page size, and bounds. The engine walks every page of one poll, gathers the records of all pages, and identifies and fans them out once (ADR-1282). Reaching `max_pages` or `max_records`, or a failed page, faults the poll naming the endpoint and lands nothing"
refs: [2789, 2757, 1282]
---

- **Date:** 2026-11-27 | **Status:** Accepted | **Pages:** [drivers](/admin/drivers/), [collection](/architecture/collection/)
- **Decision:** (#2789, a slice of #2757; the architect's ruling that the engine hard-codes no field or parameter name.)
  1. **The rule is a field of the poll's request**, `request.paginate`, on an http poll that reads `records` and sends a GET. It is declarative and names nothing of the engine's: the driver spells every field and parameter, and a platform that calls its cursor `next_page_token` is the only place that word appears (its driver's YAML).
  2. **Two styles.** `cursor`: the answer carries a value (read from the JSON body at the dotted path `cursor_from`, or from the response header `cursor_from_header`) that the next request sends back as the query parameter `cursor_param` or the request header `cursor_header`; an absent, null or empty value is the last page. `page`: the request sends an ascending number in the query parameter `page_param`, from `first_page` (1 when absent, 0 for a zero-based API); the walk stops at an answer with no records, or earlier at the answer's declared total: `pages_from` (total pages) or `total_from` (total records, which needs `size`).
  3. **Page size** is optional: `size_param` and `size` go together.
  4. **Bounds.** `max_pages` (default 100, at most 1000) and `max_records` (default 10000, at most 100000) bound one poll. Reaching either, a cursor that answers itself, a page that fails, or a page whose body does not hold the records is a fault that names the endpoint (`driver poll <driver>/<function> <METHOD> <path>`) and the page, and **nothing lands**: the walk returns no answer, so no partial fan-out reaches the outbox. This extends `MaxRecords` (2048, still the bound of an unpaged answer): a paged poll's bound is its own `max_records`, so a 3,000-room account is one collection.
  5. **Where the walk lives.** `driver.Pagination.Walk` (internal/driver/paginate.go) is the pure walk: it takes a function that sends one page and returns the first page's body with the array at `records` replaced by every page's records, in order. The collection engine supplies the page sender (`fetchPagedPoll`, internal/collection/driverpoll.go), so the walk is unit-testable with no network, and the interpreter, the identity read and the fan-out (ADR-1282) see one ordinary answer.
  6. **Example** (the shape another driver follows):

     ```yaml
     request:
       path: /api/rooms
       paginate:
         style: cursor
         size_param: page_size
         size: 100
         cursor_from: next_page_token     # or meta.cursor, or cursor_from_header: X-Next
         cursor_param: next_page_token    # or cursor_header: X-Cursor
         max_pages: 100
         max_records: 10000
     records: rooms
     ```

     A page-number API: `paginate: { style: page, page_param: page, first_page: 1, size_param: per_page, size: 100, pages_from: meta.total_pages }`.
- **Context:** A cloud platform lists thousands of rooms and devices a page at a time. Without a walk, a driver reads only the first page, and a driver per page would break the one-poll-per-account shape the conduit exists for. Declaring the rule on the request keeps the engine mechanical and lets two platforms with different field names share it. Out of scope, later and unfiled: rate-limit backoff and retry-after, and Link-header (RFC 8288) pagination. No schema migration, route or authorization changes.
