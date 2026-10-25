---
title: "Every operator resource answers to its name, and an endpoint is its component and its name"
date: 2026-10-25
status: "Accepted"
summary: "Every route that takes a uuid takes the resource's name too (#1133): an endpoint as <component>/<endpoint name> on the existing top-level /endpoints/{endpoint} (not a nested route, since a server-hosted endpoint has no component and a resource gets one canonical name), a secret or variable as <name> or <owner>/<name> with $platform for the install-wide tier, a file by filename, a principal group by name, a member or principal by username or service account name, and a grant's scope root by name or dotted address. Scope narrows the candidates before ambiguity is judged; a name no visible row answers to, or several do, is a 404 naming the reference and listing each visible match in a spelling that resolves. The uuid is tried first and every response still returns it. A docslint guard refuses a hard-coded uuid in the docs and the demos"
refs: [1133]
---

- **Date:** 2026-10-25 | **Status:** Accepted | **Pages:** [api](/architecture/api/),
  [identity and access](/architecture/identity-access/), [groups](/architecture/groups/),
  [files](/architecture/files/), [glossary](/architecture/glossary/),
  [API first](/contributing/api-first/), [CLI](/guides/cli/)
- **Decision:** (#1133, the architect's request in session.)
  1. **An endpoint is `<component>/<endpoint name>`, on the route it already had.** Every
     per-endpoint route (`GET`, `PATCH`, `DELETE /endpoints/{endpoint}`, `POST
     /endpoints/{endpoint}:retry`) takes the component in any form the component resolver takes (a
     uuid, a bare name, a dotted address), a slash, and the endpoint's name, unique on its component
     (`endpoint_component_name_key`): `boi.17c.415a.$comp.display-1/https`. The nested alternative,
     `/components/{name}/endpoints/{endpoint}`, was rejected on three grounds. A server-hosted
     endpoint has no component (and `ON DELETE SET NULL` makes one of any endpoint whose component
     goes), so a nested collection could not address every row, and AIP-122 gives a resource one
     canonical name rather than a second route that aliases the first; the four nested operations
     would have duplicated four existing ones, permission stamps and CLI commands included; and the
     same `<container>/<name>` shape is what a secret and a variable need anyway. The slash is one
     path segment, percent-encoded in a URL: the router matches the raw path, and Huma's chi adapter
     decodes the parameter, so the resolver sees the reference as typed. The generated CLI encodes
     every positional as one segment, the slash and the colon both (a filename may carry a colon,
     which the router would otherwise read as a `:verb`). A component-less endpoint's name is not
     unique, so it alone stays uuid-addressed.
  2. **A secret or a variable is `<name>` or `<owner>/<name>`.** A name is unique within its owner on
     every tier of both tables already (`secret_platform_name` and the three `secret_*_key_uuid`
     partial indexes, and their four variable twins, verified on a migrated schema), so no migration
     was needed. The cascade makes one name at several owners the ordinary case (a room overriding
     the platform's credential), so a bare name resolves only where exactly one the caller can see
     carries it, and `<owner>/<name>` always does: the owner a location, system or component in any
     tree form, or `$platform` for the install-wide tier, which has no name of its own. The `$`
     sigil is the one the address grammar reserves for platform words
     ([ADR-0089](#adr-0089-a-uuid-is-the-address-a-dotted-path-is-a-positional-lookup)), so it can
     never collide with a location named `platform`.
  3. **The rest of the operator surface.** A file answers to its filename where exactly one file the
     caller can see carries it, refusing with each candidate's uuid where several do (a filename is
     unique nowhere, so the uuid is its only unique spelling, and a uuid-shaped or dot-segment
     filename is refused at create, since it would read back as another reference); a principal
     group to its name, every group route resolving it after the all-scope check so a scoped caller
     learns nothing; a group member and every `/principals/{id}` route to a username or a service
     account's name, a word that is both naming neither, and a principal route resolving a name only
     for a caller who may list the whole directory (`principal:read:admin` on an all-scope grant),
     anyone else getting the gateway's all-scope 403 for every name, so neither a username nor a
     service name can be probed; a grant's `scope_id` to a name or a dotted address, stored as the
     uuid. What stays uuid-only has no name: an alarm, an audit row, a grant, a session or token, a
     task (its id is its content hash); a node principal is reached by its name on `/nodes`.
  4. **Scope decides before ambiguity does, and an unresolved name is a 404 naming it.** Every
     form narrows its candidates to what the caller may read first (an endpoint's components to its
     `endpoint:read` scope and to those carrying an endpoint of that name; a secret's owners to its
     `secret:read` scope and its admin tier), so an out-of-scope row sharing a name never makes an
     in-scope one ambiguous, and an absent row and an out-of-scope one give byte-identical refusals.
     The narrowing runs in SQL over the whole candidate set (the arc-scope primitive for a secret or
     a variable, a batch scope check for an endpoint's components), so a name a whole fleet shares
     costs a constant number of statements. The resolve yields the uuid, inside a write's own
     transaction where the write has one (a file delete and the principal routes resolve first, on
     properties that cannot change in between), and the route's existing read-then-action split,
     permission tier and audit row then run on that row unchanged: a name changes how a row is found,
     never what may be done to it, and an audit row keys on the uuid, never the spelling. A name no visible row answers to, or that several do, is a 404 naming the
     reference (`storage.ErrUnresolvedRef`, unwrapping to the resource's own not-found), and where
     several do it lists each in the spelling that resolves, every one a row the caller could list.
     This diverges from the tree entities, whose ambiguous bare name stays the #697 409 listing
     candidates: for these resources the issue's acceptance asked for a 404, and an address that
     names no single row of the collection reads as one. Aligning the two is #1134.
  5. **The uuid is always first and always returned.** A uuid-shaped reference is used as the id
     before any name is tried, so an id never collides with a name, and every response body still
     carries the uuid
     ([ADR-0076](#adr-0076-a-renameable-human-typed-identifier-stays-in-the-url-and-the-write-returns-the-uuid)).
  6. **The docs and the demos type names.** `TestDocsCarryNoHardCodedUUIDs` and
     `TestDemoScriptsCarryNoHardCodedUUIDs` (`internal/docslint`) fail a uuid in an address position on
     any docs page or any committed demo under `scripts/demo`, excusing an id field a response returns,
     the generated CLI reference, the append-only log entries, and an explicit allowlist that is
     empty today. A path parameter's OpenAPI example renders in the generated CLI's examples, so the
     help teaches `omniglass endpoint retry 'boi.17c.415a.$comp.display-1/https'`, not `<id>`.
- **Stated limits, recorded rather than hidden:**
  1. **A bare name lands on whatever single row the caller can see by it.** A secret rotation by a
     bare name, where the row the operator meant is gone and another principal has sealed a secret of
     that name within the caller's view, rotates that other secret. That is the positional honesty
     ADR-0089 already accepts for a dotted path, and the mitigation is the same: an update answers
     with the owner it landed on (`owner_kind`, `owner_id`, `owner_name`; a delete has no body to
     answer with), after the fact, and automation that must not drift addresses `<owner>/<name>` or
     the uuid. Refusing a bare name on a write would have
     broken the acceptance this decision was built to (`omniglass secret update demo-panel-login`).
  2. **A qualified name's slash crosses every hop percent-encoded.** The server decodes it; whether
     the chart's default Traefik ingress passes it through is unverified and tracked as #1138. The
     uuid form is unaffected.
  3. **The CLI refuses a `.` or `..` positional**, a dot segment a normalizing proxy would rewrite
     into the route above it; no name, address or filename can be either.
- **Context:** The tree entities had answered to a name and a dotted address since #627, but an
  endpoint, a secret, a variable, a file and a principal group took only a uuid, so the #1131 demo
  looked each component's uuid up with `jq` before it could retry an endpoint or rotate a secret, and
  the CLI guide said "endpoints are addressed by id". A non-uuid reference on those routes used to
  reach Postgres as a uuid cast and answer 500. The adversarial security review this slice ran
  (name resolution against scope, existence disclosure, the window between resolve and act, secret
  name enumeration) found the core authorization sound and shaped the rest: the principal routes'
  directory precondition, the in-SQL narrowing, the refused filenames, the dot-segment refusal, the
  owner a write answers with, and the human-only lookup the break-glass `set-password` now revokes
  sessions by.
