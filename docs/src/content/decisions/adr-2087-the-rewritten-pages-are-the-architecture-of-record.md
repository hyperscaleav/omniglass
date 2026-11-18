---
title: "The rewritten pages are the architecture of record; the architecture pages archive"
date: 2026-11-18
status: "Accepted, amended by ADR-2087.2"
summary: "The architecture pages freeze as dated history, beside the ADRs and the build log, and the vocabulary denylist exempts them. The overview and guide pages, rewritten one per slice in the vocabulary table's words, become the architecture of record: they carry the Design, Partial and Built badges, the badge-fence lint and the /ship-slice status gate read them, and CLAUDE.md and docs-with-everything point at them. Generated reference pages keep following the code"
refs: [2087, 2028, 2029]
---

- **Date:** 2026-11-18 | **Status:** Accepted | **Pages:** [docs with everything](/contributing/docs-with-everything/)
- **Decision:** (#2087.) The record moves to the pages written in the vocabulary's words.
  - **The architecture pages archive.** They move under an archive section whose banner says they predate
    the vocabulary and points to the [glossary](/reference/glossary/). They are not edited for a rename
    again: the vocabulary denylist exempts the archive as it exempts the decision log, because an archived
    page's words were true on its date. The rename sweeps cover code, API, schema, the console and the live
    docs, not the archive.
  - **The rewritten pages are the record.** The overview and the guide pages are rewritten one page per
    slice from the vocabulary table (#2029). Each rewritten page says how a surface works and how to use
    it, in present tense, and carries the status badge (`Design`, `Partial`, `Built`, `Diverged`) the
    architecture page used to carry. The badge-fence lint, the implementation-status grid and the
    `/ship-slice` status gate read the rewritten pages. A slice that ships a surface moves its page's badge
    in the same PR, as before; a page not yet rewritten carries no badge, and its archived predecessor
    stays the reference until it is.
  - **History stays dated.** ADRs and the build log stay one file per entry and are not swept. An ADR still
    lands with any divergence from a rewritten page.
  - **Generated pages follow the code.** The glossary, the CLI and MCP references and the OpenAPI spec
    regenerate from the code and the vocabulary table, unchanged.
  - **The archive PR changes the doctrine with it.** CLAUDE.md's spine link, docs-with-everything and
    `/ship-slice` move to the new record in the same PR that archives the pages, and each guide or overview
    page gets a rewrite ticket.
- **Context:** The taxonomy epic (#2028) retires words that about 40 architecture pages use thousands of
  times, and every rename paid twice: once in the code and once in a grep sweep of prose that leaves seams.
  The rejected alternative was to let the ADRs and the build log alone be the record, with no status on any
  page. That keeps nothing to sweep, but it drops the badge, which is the one place a reader learns what is
  built today without reading a log; the rewritten pages keep the badge and keep it on a page the
  vocabulary already governs.
