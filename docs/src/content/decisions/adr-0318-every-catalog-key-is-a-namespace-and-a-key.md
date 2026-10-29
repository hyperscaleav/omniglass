---
title: "Every catalog key is a namespace and a key"
date: 2026-10-29
status: Accepted
summary: "Every catalog that carries `official` keys its rows by `(namespace, name)`. `og` is the platform's namespace and only the boot seed writes it; `local` is the operator's default; any other namespace is the operator's to use. A bare name resolves `local`, then `og`, so an operator's key shadows a shipped one of the same name and a release can never write an operator's row. The pair is two columns (nothing parses a joined key); `namespace/name` is only its rendering, and a URL carries it as two path segments."
refs: [318, 1431, 1432, 1433, 1434, 1307, 144]
---

- **Date:** 2026-10-29 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/),
  [alarms and actions](/architecture/alarms-actions/)
- **Decision:** (#318, approved 2026-10-29; amends [ADR-0144](/architecture/decisions/#adr-0144-alarms-are-raised-by-event-rules-at-a-severity-level-from-a-ranked-registry)'s clash rule.)
  1. **The identity is the pair.** The 18 catalogs carrying `official` (the signal vocabulary, the
     types, the alarm and notification registries, the catalog, and roles) gain a `namespace`
     column beside `name`. A namespace is one kebab segment, like a name.
  2. **`og` is the platform's, and only the seed writes it.** A shipped row lands in `og`, an `og`
     row is always official (a CHECK holds it), and the seed's conflict update applies only to an
     `og` row, so a release never writes an operator's row. An operator's row is `local` unless they
     name another namespace. `official` stays its own column: the namespace says who named a key,
     `official` who stands behind it.
  3. **A bare name resolves `local`, then `og`** (the search path, #1432), in every reader, and
     across catalogs: an operator's key shadows a shipped one of the same name, and the shipped one
     stays reachable by its qualified name. It generalizes [ADR-1307](/architecture/decisions/)'s
     rule rather than adding a second.
  4. **Nothing parses a key.** Storage, the telemetry wire (#1433) and API bodies carry the pair as
     two fields. `namespace/name` is the rendering for display, the CLI and docs, and a URL carries
     the pair as exactly two path segments (#1434), so a key never collides with a child route and
     nothing is escaped.
- **Context:** the seed upserted every catalog by name, authoritative on conflict. Of 16 such
  upserts, 14 took an operator's row of a shipped name over (a custom role's permissions replaced
  under its holders among them), and two refused and stopped the boot. #1307 answered the
  cross-catalog half; a namespace answers both, and gives community and partner publishers
  (`crestron/`) a place to ship without colliding with the platform's vocabulary.
- **Not chosen:**
  - **A separator in the stored name.** `:` and `::` break the API's `:verb` routes (chi ends a path
    parameter at the first colon); `@` and `+` do not survive the console client's escaping; `.` is
    the dotted address's. The pair in columns needs no separator at all.
  - **A namespace segment that defaults to the platform's.** A bare create lands in `local`, so a
    namespace called `default` would name the wrong side.
- **Built so far:** the columns, the `og`/`local` backfill, and the seed writing only `og` (#1431).
  Uniqueness stays on `name` until the search path lands (#1432). Until then an operator's row that
  holds a shipped name keeps it and the shipped row is not written on that install, nor are its
  children (a product's contract); a shipped row that names it (a child type's parent, a product's
  driver) binds to the operator's row, which #1432 ends by resolving the seed's references inside
  `og`. A severity rank a differently named operator level holds still stops the boot.
