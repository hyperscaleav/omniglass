---
title: "every operator resource answers to its name (#1133)"
date: 2026-10-25
refs: [1133]
---

Every route that took only a uuid now takes the resource's name as well
([ADR-1133](/architecture/decisions/#adr-1133-every-operator-resource-answers-to-its-name-and-an-endpoint-is-its-component-and-its-name)).
An endpoint is `<component>/<endpoint name>` on its existing `/endpoints/{endpoint}` routes and its
`:retry`; a secret or a variable is its name, or `<owner>/<name>` with `$platform` for the
install-wide tier; a file is its filename; a principal group is its name; a group member and every
`/principals/{id}` route take a username or a service account's name; and a grant's `scope_id` takes a
name or dotted address, stored as the uuid. Secret and variable names were already unique within
their owner on every tier, so no migration rode along.

One primitive file holds the resolvers (`internal/storage/name_ref.go`), each built from the
component resolver's own lookup (`loadByRef`, `loadByRefWithin`) and the existing scope checks:
candidates are narrowed to what the caller may read before ambiguity is judged, the resolve runs in
the request's transaction and yields the uuid, and the route's read-then-action split, permission
tier and audit row then run on that row unchanged. A name nothing visible answers to, or several do,
is `ErrUnresolvedRef`, a 404 naming the reference and listing each visible match in a spelling that
resolves. Two latent defects went with it: a non-uuid reference on these routes reached Postgres as a
uuid cast and answered 500, and a secret or variable update wrote back through the reference it was
given rather than the row it resolved.

The generated CLI shows a path parameter's OpenAPI example in its help and sends every positional as
one path segment (a slash and a colon both encoded), so `omniglass endpoint retry
'boi.17c.415a.$comp.display-1/https'` and `omniglass secret update demo-panel-login --fields ...` work
as printed. The guides, the CLI guide and the http-logins demo now type names, and
`TestDocsCarryNoHardCodedUUIDs` / `TestDemoScriptsCarryNoHardCodedUUIDs` keep it that way. The console
still addresses everything by uuid (ADR-0089), so it moved only where a path parameter was renamed.

Proven in storage against real Postgres (each form, ambiguity judged in scope, an out-of-scope row
refusing byte-for-byte as an absent one, the write split and the audit key unchanged), over HTTP (the
same through every changed route, and a scoped caller's refusal leaking no uuid), and end to end
through the CLI binary's own commands against a live server (`internal/cli/name_address_test.go`).
