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
- **Built so far:**
  - **The columns** (#1431): `namespace` on the 18 catalogs, backfilled from `official`; the seed writes
    only `og`.
  - **The search path** (#1432). Uniqueness is `(namespace, name)` on every catalog but
    `severity_level`, and the seed upserts on it, so a shipped name an operator holds lands beside
    theirs. `<table>_in_effect()` is what a bare name reaches (local, then the og rows local does not
    shadow, across the three signal catalogs for those), and every by-name read in the storage
    package goes through it, `refRows` (by uuid any row, by name the search path) or `catalogRows`
    (one namespace), held by `TestNoByNameLookupSkipsTheSearchPath`, a scan of the package's SQL. A
    shipped row's references (a product's vendor, driver and type, a child type's parent, a command's
    target, a rule's key and events, a product's contract) resolve inside `og`, and a shipped driver
    validates against `og`.
  - **The wire's namespace** (#1433): a telemetry sample and a push item carry `namespace`, read
    in that namespace only when set. A sample onto the row its bare name already reaches is written
    bare; one onto any other row lands there, held to that row's writer class, and is judged by no
    event rule, marks no calc rule and settles no command, since each reads a key by its bare name.
    A driver emit naming its namespace is #1456.
  - **Rulings (#318, 2026-10-29):** a role refuses ambiguity rather than shadow (a name both `local`
    and `og` hold reaches neither, so its holders lose the role, never gain the wrong one; operators
    have no role-create path yet, and exact role references come with one), and severity levels are
    overridden, never shadowed, keeping names and ranks unique install-wide.
  - **Divergences, logged:** a grant naming an ambiguous role is refused as an unknown role (422)
    rather than the ruling's 409 naming both, since operators cannot create a role yet; a shipped
    rule or command type is validated against `og` at seed, but at run time follows the search path
    like every reader, so a rule on a shadowed key judges the operator's series; which command types
    target a key matches any row of its name, since a shipped command type's target is pinned in
    `og`; and the reserved-key exception `ResolveNameLane` makes is not yet in `<table>_in_effect()`
    (#1447).
  - **An operator's own create or rename never takes a name `og` holds** (a trigger raises the
    name's unique violation, as before): a shadow arises only from a release.
