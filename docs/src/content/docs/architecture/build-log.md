---
title: Build log
description: "The slice-by-slice build history: one entry per merged vertical slice, oldest first."
---

Each entry below is one merged vertical slice, oldest first, logged as it landed. The live
map of what is built sits on [implementation status](/architecture/status/); this page is
the history behind it. Entries are append-only and quote the vocabulary of their day, so
retired terms appear here exactly as they shipped.

The code is built one **vertical slice per PR**, each a thin cut through the whole stack rather than a
horizontal layer. Slices are logged here as they land; a page's badge only flips once *all* of its
capabilities ship, so an early slice can prove a seam without moving any page off `Design`.

- **Slice 1: the walking skeleton.** The single binary boots in two run modes. `omniglass migrate`
  applies an embedded [dbmate](/architecture/storage/) migration (pure DDL, run-once, idempotent) against
  a BYO Postgres, and `omniglass server` serves `GET /api/v1/healthz`, which pings the database through
  the [Storage Gateway](/architecture/storage/) seam (the only DB path, an interface from day one) and
  reports `{status, db}`. The [HTTP API](/architecture/api/) is Huma over chi; the OpenAPI 3.1 document
  is generated from the Go structs (`make gen`), the source the typed clients are generated from in later
  slices. Proven by a testcontainer integration test (migrate applies clean, the gateway ping succeeds,
  healthz returns db ok) and an end-to-end test that builds and runs the real binary. Deferred to later
  slices: messaging (NATS / JetStream), the worker and outbox, [identity and access](/architecture/identity-access/)
  (Slice 2), the [collection](/architecture/collection/) engine, and the rest of the generation pipeline.
- **Slice 2: the auth and gateway foundation.** The [identity and access](/architecture/identity-access/)
  schema (principal with per-kind human and service, credential, role, principal_grant, and audit_log;
  uuidv7 keys), the four official roles seeded idempotently at boot, and `omniglass bootstrap <username>`,
  which creates the first owner (an `owner@all` grant plus a bearer credential) directly and idempotently.
  A bearer token resolves through the [Storage Gateway](/architecture/storage/) to its principal and
  grants, which flatten through the role index (inheritance, wildcards, the `:read` floor) into a
  capability set. Two seams every later route reuses: the **capability** fast-reject (401 unauthenticated,
  403 missing capability) and `GET /api/v1/auth/me` (principal, permissions, grants). `GET /api/v1/roles`
  is gated by `role:read`. Proven by testcontainer integration and end-to-end tests (bootstrap
  idempotency, the 401 and 403 paths, the auth/me contract) plus pure unit tests of the capability logic.
  Deliberate thin cuts this slice: bearer-token auth only, and scope resolves `owner@all` to all (the
  per-action `visible_set` resolver lands when there are entities to scope). Deferred: password and OIDC
  auth, the first-class agent principal, and CDC-driven cache invalidation of the role index.
- **Slice 3: locations and the per-action scope resolver.** The first scoped [core entity](/architecture/core-entities/),
  and where the [ABAC scope](/architecture/identity-access/) deferred from Slice 2 lands. The `location_type`
  registry (the only shape-definer, since a location has no template) seeds the four official types at boot,
  ranked (campus/building/floor/room, spaced by ten); `location` is a name-addressable, variable-depth tree
  (`parent_id`) whose type is a foreign key. `POST/GET/PATCH/DELETE/list /api/v1/locations` each declare a
  `location:<action>` capability and resolve the caller's per-action `visible_set` from their grants and the
  role index, which the [Storage Gateway](/architecture/storage/) expands (a recursive subtree walk) into the
  row filter and applies on every query, writing the `audit_log` row in the same transaction. The over-permit
  fix is real: only the grants whose role carries the action contribute scope, so a read-anywhere principal
  with a narrow write grant cannot write outside it. The three-way status split holds: out of read scope is a
  non-disclosing 404, readable-but-out-of-action-scope is 403, and a delete is refused (409) while the location
  still has children. The `location_type` registry that classifies a location is itself readable at `GET /api/v1/types/location` (ranked, gated by `location:read`), which the operator console type picker lists. Proven by a pure resolver unit suite, a testcontainer integration test of the gateway
  split and audit, and an end-to-end HTTP test (scoped subtree listing, the 404, the capability 403, full owner
  CRUD). Deliberate thin cuts: scope kinds resolved are `all` and `location` only; the `location_type` registry
  seeds official rows only; `rank` orders and signals hierarchy but does not constrain nesting; update patches
  `display_name` and `location_type` (rename and reparent deferred); occupancy counts child locations only
  (placed systems and components join when they land). Deferred: system and component entities (Slice 4),
  group and dynamic-group scope (Slice 5), operator-defined location types via the namespace shadow, hard
  containment rules, and any `LocationTemplate`.
- **Slice 3a: the generated CLI.** The second stage of the [generation pipeline](/contributing/api-first/)
  lands: `make gen` now runs `cmd/cligen`, which reads the committed `api/openapi.json` and emits the cobra
  command tree (`internal/cli/api_gen.go`, committed and reviewed like the spec). One command per operation,
  derived from the AIP path and method: `omniglass location list/get/create/update/delete`, `role list`,
  `auth me`, `healthz`; path parameters are positional args, the request body becomes `--flags`, and `--help`
  plus the example come from the operation's summary and description. A `:verb` custom method maps to
  `<resource> <verb> <id>` generically, so future custom methods surface with no generator change. The
  generated tree composes with the hand-written commands (the `server`/`migrate` run modes and the trusted
  `bootstrap` lane) on one root through a stable seam (`internal/cli/api_hooks.go`: the JSON-over-HTTP client
  and the shared `--server`/`--token` flags), so regeneration never touches a hand-written command. The CLI is
  a client of the API like any other: it carries a bearer token and the server enforces the same capability
  and scope. Proven by an end-to-end test that builds the real binary, bootstraps an owner with the
  hand-written command, and drives the generated location commands against a live server (create, scoped list,
  get, a non-disclosing 404 as a non-zero exit, delete, and `--help`). Deliberate thin cuts: JSON output only
  (no table rendering), and a generic client rather than a per-type typed one. Deferred: the typed SPA client
  and UI (Slice 3b), shell completion, multi-server contexts, and the YAML JSONSchema target.
- **Slice 3b: the operator console.** The last stage of the [generation pipeline](/contributing/api-first/)
  and the first browser surface: a SolidJS SPA (Vite, daisyUI 5 on Tailwind 4, `@solidjs/router`,
  `@tanstack/solid-query`, `openapi-fetch`) `go:embed`'d into the binary and served under `/web`. `make gen`
  now also emits the **typed SPA client** (`openapi-typescript` to `web/src/api/schema.gen.ts`), so the
  console cannot drift from the API. The embed uses the build-tag-with-fallback pattern (`internal/webui`): a
  bare `go build`/`go test` needs no `dist/` and serves a build-the-console placeholder, while `make build-web`
  runs the Vite build and compiles with `-tags web` to embed the real shell. The visual system is the
  "Omniglass Console" design ported faithfully (the `theme.css` tokens and component classes, the nav rail,
  top bar, density/theme tweaks, and Home). **Locations is the first live view**: list, detail, and create
  through the typed client, gated by a bearer-token login and the AuthGuard; the other nav sections render
  honest stubs until their backends land. Proven by `internal/webui` unit tests (the SPA-fallback routing
  against a fake FS, and the real embed under `-tags web`), an API mount test (`/web` serves the shell and the
  bare `/web` redirects), and a vitest suite over the locations data layer. Deliberate thin cuts: only
  locations is wired live; styling is daisyUI (two brand themes via the plugin) with Kobalte deferred until the
  first interactive widget needs it; auth is bearer-token paste, not a full login. Deferred: the mock-data screens
  (alarms, systems, components, dashboards) as their backends land, HTTP/2 (h2c) and the SSE live relay, and
  full auth UX.
- **Slice 4a: the system tier.** The second scoped [core entity](/architecture/core-entities/): a `system_type`
  registry (seeded official, ranked, like `location_type`) and `system` as a name-addressable, variable-depth
  tree (`parent_id` subsystems), optionally located at a location (`location_id`) and classified by its type.
  Full scoped CRUD (`POST/GET/PATCH/DELETE/list /api/v1/systems`), gated by `system:<action>`, with the same
  per-action `visible_set`, three-way 404/403 split, and in-transaction audit as locations. The scoped-tree
  recursive walk is now a **shared primitive** (`internal/storage/scopetree.go`): locations were refactored
  onto it and systems consume it, so the over-permit-safe subtree filter is written once. Proven by the pure
  resolver unit cases for the `system` kind, a testcontainer integration test (subtree scope, the 404/403
  split, occupancy, FK faults, located-at, audit), and an end-to-end HTTP test plus the generated
  `omniglass system` CLI. Deliberate thin cuts: each entity is scoped by its **own** subtree (the cross-tier
  cascade, a location scope also covering its systems, is a later slice); reparent and located-at editing are
  deferred (create-time only). Deferred: components (Slice 4b), the cascade, templates, datapoints, and the
  systems UI (console section stays a stub for now).
- **Slice 4b: the component tier, and a generic scoped-CRUD primitive.** The estate is complete: `component`
  (the leaf) joins locations and systems, with a `component_type` registry and a tree that belongs to a
  primary `system_id` and is located at a `location_id`. The per-entity boilerplate is now a **shared
  generic** (`internal/storage/scopedcrud.go`): the read, resolve, and delete paths (list/get/resolve-for-action/
  delete-with-occupancy/by-name, all scope-aware and audited) are written once over a `scopedConfig[T]`, and
  locations and systems were refactored onto it; each entity keeps only its own create/update (which differ by
  the foreign keys they resolve). So a new scoped entity is a registry, a thin create/update, and a route file,
  not a mirrored copy. And the [authorization conformance suite](/architecture/identity-access/) made adding
  component **one registry line**: the full security matrix (capability 403, the over-permit scope 403, the
  non-disclosing 404, in-scope success) and the no-unguarded-route guard now cover all three entities
  automatically. Proven by the pure resolver `component` case, a testcontainer integration test (subtree scope,
  the split, occupancy, belongs-to + located-at FK resolution, faults, audit), the generic authz conformance
  over location + system + component, and the generated `omniglass component` CLI. Thin cuts as the other
  tiers: own-subtree scope (no cross-tier cascade), reparent/rebind/relocate deferred, components UI deferred.
- **Identity slice 1: password auth and the session cookie.** Humans now sign in to the console with a
  username and password, the local-password method the [identity model](/architecture/identity-access/)
  designs: a `password` credential (argon2id, PHC-encoded, one per principal). `POST /api/v1/auth/login`
  verifies it and sets an httpOnly, `SameSite` session cookie, so the console no longer holds a token
  (services and the CLI still send a bearer header); `POST /api/v1/auth/logout` revokes the session and
  clears the cookie, and
  the authn middleware accepts either. `omniglass bootstrap --password` sets the first owner's password.
  Proven by argon2 unit tests, a storage round-trip, a real-binary login integration test, and a browser
  e2e that signs in through the form. Thin cuts: self profile edit and change-password (slice 2), admin
  user CRUD and role assignment (slice 3); email invite and forgot-password are later.
- **Identity slice 2: the self-service profile.** A signed-in operator manages their own account, whatever
  their role: `PATCH /api/v1/auth/me` edits their own display name (email stays administrator-set), and
  `POST /api/v1/auth/me:changePassword` (the first AIP `:verb` custom method on the surface) verifies the
  current password and sets a new one, reusing the slice-1 argon2 primitive. Both are authn-only and
  self-scoped (they touch only the caller's own principal), so they join the small ungated allow-list
  alongside `GET /api/v1/auth/me` rather than carrying a
  capability. The console gains a **Your profile** page (profile form, change-password form, and a read-only
  Access panel that teaches the principal, grants, and permissions it operates on); the CLI gains
  `auth update-profile` and `auth change-password` from the generator. Proven by a storage round-trip, a
  real-binary integration test (wrong current password 403, short new 422, rotation, self-scope), and the
  client hook units. Thin cut: changing a password does not revoke existing sessions (a later hardening);
  admin user CRUD and role assignment are slice 3.
- **Identity slice 3a: the admin principal directory.** The first surface to exercise the already-seeded
  `principal:*` capability: `GET /api/v1/principals` (with a `kind` filter) and `GET /api/v1/principals/{id}`
  list and read every principal with its grants, and `POST /api/v1/principals` creates a human with an
  optional initial password (argon2id, reusing the slice-1 credential; no new migration). All three are gated
  by a `principal` capability that resolves **only at all-scope**: a principal is not a scope-tree entity, so
  a location or system grant confers nothing and the gateway answers a non-all scope with 403, proven both
  ways. Responses carry credential-free profiles and grants, so no secret ever leaves the API. The console
  `/users` stub becomes a real **Users** directory (grid, detail panel, and a create form); the CLI gains
  `principal list` / `get` / `create`. Proven by a storage round-trip, a real-binary integration test
  (all-scope allow, scoped-deny 403, duplicate 409, short-password 422, secret redaction, created-human
  login), the route-gating guard, and the client data-layer units. Thin cuts: no `created_at` column in the
  directory yet, and create is human-only. Deferred: update / disable / delete a principal, role assignment,
  the owner-invariant trigger, and service-account and credential management.
- **Identity slice 3b: admin update a principal.** `PATCH /api/v1/principals/{id}` edits a human's display
  name, email, and **username**, gated by `principal:update` (all-scope). Renaming is safe by construction:
  nothing keys on the username (credentials and grants reference the `principal.id` uuid), so a rename
  re-homes the login rather than breaking it. A non-human target is 422, a clash 409, an unknown id 404, a
  location-scoped admin 403. The console Users detail panel gains an **Edit** form; the CLI gains
  `principal update`. Proven by a storage round-trip (the rename re-homes authentication), a real-binary
  integration test (allow, scoped-deny 403, 409, 404, 422), and the client unit. Deferred: disable / delete,
  role assignment, and editing a service account's label.
- **Identity slice 3c: role assignment and the owner invariant.** `POST /api/v1/principals/{id}/grants`
  assigns a role at a scope and `DELETE /api/v1/principals/{id}/grants/{grantId}` revokes one, gated by
  `principal_grant:create` / `:delete` (all-scope). This is how a fresh user gets permissions. The
  **owner-invariant trigger** (ADR-0006, now resolved) lands here: a `DEFERRABLE INITIALLY DEFERRED` Postgres
  constraint trigger refuses to leave zero `owner @ all` grants at `COMMIT`, so revoking the last owner is a
  409 while a one-transaction owner swap still passes (mapped from the custom SQLSTATE `OG001` to
  `ErrLastOwner`). Bad inputs are caught: duplicate 409, unknown role 422, a scoped grant with no id 422,
  unknown grant 404. The console detail panel gains a grant chip **x** (revoke) and an **add-grant** form
  (role picker, scope kind, scope id); the CLI gains `grant create` / `grant delete`. Proven by a migration
  round-trip, a storage test (grant, revoke, the last-owner refusal, the swap), a real-binary integration
  test, and the client units. Deferred: disable / delete a principal, group grants, and editing a grant in
  place (revoke and re-grant instead).
- **Identity slice 3d: disable and enable a principal.** `POST /api/v1/principals/{id}:disable` and
  `:enable` (AIP custom methods, gated `principal:update`, all-scope) soft-disable an account: a new
  `active` flag on `principal`, and `AuthenticateBearer` / `AuthenticatePassword` refuse a disabled
  principal, so it can neither sign in nor use a token. Its rows (and the audit trail that references it)
  are kept, which is why the model is disable, not delete: `audit_log` references the principal, so an
  actor that has acted cannot be removed. A **last-active-owner guard** refuses to disable the final active
  `owner @ all` (409), mirroring the grant trigger for the active flag. The console shows an **inactive**
  badge and a **Disable / Enable** toggle; the CLI gains `principal disable` / `enable`. Proven by a
  migration round-trip, a storage test (both auth paths refused, enable restores, last-owner refusal, the
  swap), and a real-binary integration test. Deferred: hard delete (would need an audit-actor
  `on delete set null`, a separate decision) and bulk operations.
- **Identity slice 3e: the grant builder (console).** A UI-only refinement of slice 3c: the user card's
  three stacked selects become a **filter-bar-style scope editor**. A keyboard-staged combobox (type a
  role, Tab/Enter to the scope kind, then the entity as an indented tree) commits each grant as a chip,
  and the whole set of changes is staged locally and applied only on **Save** (stage, preview, save), so
  there are no accidental or unclear edits: removing an existing grant marks it (dimmed, undoable) instead
  of firing a live `DELETE`, and a pending-diff preview shows exactly what Save will grant and revoke. No
  new API: it drives the slice-3c `POST` / `DELETE` grant routes, applying adds before removes so an owner
  swap never trips the last-owner guard mid-batch. The staging core (draft, per-chip state, add/remove
  diff, stage validation) is a pure unit-tested module (`lib/grantdraft`); the `GrantBuilder` component
  takes its roles, entity trees, and save mutation as props, proven by a component test that drives the
  keyboard pipeline and asserts the saved diff. Deferred: previewing a grant's *effective access* across
  the scope subtree (#89) and group grants.
- **Console: unified button vocabulary.** Buttons across the console moved from ad-hoc daisyUI classes
  (`btn-primary`, `btn-ghost text-error`, and an inconsistent disable/edit pairing) to a small set of
  **semantic intent classes** defined in `app.css` (`btn-action`, `btn-quiet`, `btn-danger`, `btn-warn`,
  `btn-ok`), `@apply`-composed from the daisyUI theme tokens so a future custom theme restyles every
  button from one place. A `style-guard` unit test scans the source and fails on a raw `btn-primary` /
  `btn-ghost`, pinning the vocabulary against drift. No behavior or API change: purely how buttons are
  styled. See the [design system](/contributing/design-system/#button-vocabulary). Follow-on: custom
  theme switching beyond the two brand themes.
- **Identity: disabled-account sign-in message.** A disabled account that signs in with the **correct**
  password now gets an explicit "This account is disabled. Contact your administrator." (a distinct
  `403`), instead of the generic invalid-credentials message, so a locked-out operator knows to ask an
  admin rather than assuming a wrong password. The disclosure is **oracle-safe by construction**:
  `AuthenticatePassword` drops the `and pr.active` filter, always runs the argon2 verify (against the
  real hash, or a dummy hash of matching parameters on a miss, so there is no early-return timing leak),
  and only branches to the new `ErrAccountDisabled` **after** the password verifies. So a wrong password
  (or an unknown user) against a disabled account is indistinguishable from any other bad credential:
  same generic `401`, same work. The bearer/token path is unchanged. Proven by storage tests (correct
  password on a disabled account -> `ErrAccountDisabled`; wrong password -> `ErrBadCredentials`), a
  real-binary api test (disabled + correct -> `403`, disabled + wrong -> `401`), and a web unit. Deferred:
  **auditing** the disabled-login attempt (#97), which belongs at the api/outbox layer, not the gateway
  read, to avoid a write-on-read and an attacker-append vector.
- **Identity: root-excluded grants and the deploy role.** A grant gains an optional `exclude_root` modifier
  that narrows its **modify** actions (update, delete) to the root's descendants: the holder can create under
  and edit within the subtree, but a `PATCH` / `DELETE` on the root itself is a **403** (readable, outside the
  write scope). Read and create-placement keep the root, so a `POST` under the root and a `PATCH` on a child
  still succeed. A new **deploy** official role (create + update on location / system / component, read via the
  viewer floor) is the integrator / field-tech grant: `deploy @ location:room-42 (exclude_root)` populates a
  room without touching its record. The modifier lives on `principal_grant`, resolved in one predicate
  (`inScopeTree`) shared by all three tree entities, not a new scope kind (ADR-0009); an inclusive grant on the
  same root wins. Proven by scope unit tests (per-action classification, inclusive-wins) and a real-binary API
  test (read root 200, create-under-root 201, update child 200, update root 403, delete 403). API and CLI carry
  `exclude_root`; the grant-builder toggle is a fast-follow (#99).
- **Console: scope-aware per-row action gating.** The read side now annotates every inventory row with the
  scope-aware `actions` it permits (create-a-child / update / delete), computed from the **same** per-action
  `visible_set` the gateway enforces (a new batch `InScopeIDs` primitive answers a whole page in one query per
  action). The console's `ListView` gates each row's Add-child / Edit / Delete affordance on `row.actions`
  rather than the coarse `can(...)` capability, so a scoped operator (e.g. `viewer@all + writer@root`, or a
  `deploy` grant) sees only the buttons the server would actually allow, per row, with no client-side scope
  math. `can(...)` stays the coarse nav/section hint; the server remains the only authority. Proven by a
  storage test (batch membership across all / rooted / exclude-root scopes) and a real-binary API test (owner
  gets every action on every row, a viewer none, a `deploy @ root (exclude_root)` gets create-only on the root
  and create+update on descendants). Thin cut: the list carries `actions`; single-item GET annotation is a
  follow-on (the blades render from the list rows).
- **Identity: impersonation (view-as and act-as).** An owner or all-scope admin holding
  `principal:impersonate` can `POST /principals/{id}:impersonate` to mint a bounded, revocable token to
  **view as** (read-only) or **act as** (full) another principal, and `POST /auth/me:stopImpersonation` to
  end it. Two guarantees: the **escalation guard** (`rbac.Set.Covers`) refuses impersonating a principal
  whose capabilities exceed the caller's, so no capability is ever gained; and **dual-actor audit**, a new
  nullable `audit_log.real_actor_principal_id` (threaded via a request-scoped context value, no gateway
  signature churn) records the true admin behind every impersonated mutation. The token is an
  `impersonation_session` (its own table, not a credential); `authn` resolves it on a bearer miss to the
  target, and `require()` refuses every non-read action in view-as. Self and nesting are refused; disabling
  either party kills the session on its next request (ADR-0008 / ADR-0010). Proven by a storage round-trip
  and a real-binary API test (act-as writes in the target scope with a dual-actor audit row, view-as GET 200
  / PATCH 403, a lesser admin cannot impersonate the owner, self 422, a viewer capability-gated, stop kills
  the token, a split-grant admin cannot act-as into a disjoint scope). The console ships an **Impersonate**
  action (View as / Act as) on the user card and a persistent **acting-as banner** with Stop. Deferred:
  act-as within a scoped admin's own scope by intersection (#101; act-as currently requires all-scope over
  the target's write capabilities) and re-checking the guard per request (bounded by the TTL + revoke for now).
- **Identity: grant scope operators (`scope_op`).** The `exclude_root` boolean generalizes into a `scope_op`
  operator on `principal_grant` (ADR-0011): **`subtree`** (root + descendants, the default, == old
  `exclude_root=false`), **`subtree_excl_root`** (descendants only for update/delete, root kept for
  read/create, == old `exclude_root=true`), and **`self`** (exactly the root row for read/update/delete, no
  descendants and no create-placement, a leaf-lock) which is **net-new**, a grant on exactly one node. The pure `scope.Resolve` gains a `SelfIDs`
  set (matched by id equality, never subtree-expanded; a `self` grant re-adds a root a `subtree_excl_root` grant
  stripped), and the three gateway walks (`inScopeTree`, `InScopeIDs`, `scopedListSQL`) gain a self arm. The
  migration recreates the dedup index to include `scope_op` (fixing a latent collision where two grants
  differing only by operator clashed) and threads `scope_op` through `RevokeGrant`'s audit SELECT (previously
  dropped). The **grant builder** gains an operator stage (role -> kind -> entity -> operator, subtree
  pre-selected), and each scoped chip shows its operator glyph (= / ≥ / >), so #99 (setting the modifier from
  the console) ships here too. Proven by scope unit tests (self classification, self re-adds an excluded root,
  self-under-subtree is redundant), a storage round-trip (bad operator refused, a self grant persists alongside
  a subtree grant on the same root), a real-binary API test (a `self` grant reads and updates its node,
  descendants 404, list returns only the node), and web unit + component tests (operator in the grant identity,
  an operator change diffs as revoke + grant, staging a self grant sends `scope_op: self`). The act-as scope
  intersection (#101) is **not** subsumed: it is plumbing, orthogonal to how a scope is expressed.
- **Identity: owner accounts are un-impersonatable.** A principal holding `owner @ all` cannot be impersonated
  by anyone, including another owner, in either mode (ADR-0012): a target-side check in the `:impersonate`
  handler before the mode branch (403), reusing the owner-invariant's `role='owner' and scope_kind='all'` lane.
  The escalation guard (`Covers`) already blocked a lesser admin from an owner, but `owner.Covers(owner)` was
  true, so owner-impersonates-owner was possible; this removes the highest-trust-account takeover vector
  explicitly. Impersonate stays gated by `principal:impersonate` **swept by `principal:*`** (holding it already
  lets a caller create and use its own principals, so impersonate confers no new reach there). Act-as scope
  intersection (#101) is **dropped**: act-as stays all-scope-only. Proven by the real-binary API test (an owner
  cannot view-as or act-as another owner, 403; admin-to-owner stays 403; a non-owner target still works).
- **Identity: a grant cannot exceed the granter.** Grant creation is refused (403) when the granted role's
  capabilities are not covered by the granter's **all-scope** capabilities (`rbac.Set.Covers`, ADR-0013), so no
  caller can promote anyone (including itself) above its own tier: an **admin cannot grant `owner`** (`*:*`) and
  therefore cannot self-promote to superuser. `CreateGrant` previously checked only all-scope
  `principal_grant:create`, so admin could grant itself `owner@all` and log in as a superuser; the guard lives
  in the `create-grant` handler, mirroring the impersonation escalation guard, and only all-scope grants count
  (a narrowly-scoped capability cannot be conferred estate-wide). The deliberate stance: **admin is bounded on
  purpose**, the top management role, not the superuser; `owner` is the break-glass superuser and the
  owner-invariant anchor. Proven by a real-binary API test (admin cannot grant owner to a user or itself, 403;
  admin can grant viewer/operator it covers; owner can grant owner). Role editing will need the same rule.
- **Identity: the Roles view and role metadata.** Roles gain a `display_name` and `description` (additive
  migration, seeded for the five official roles), and `GET /roles` now returns them plus each role's
  **effective permissions** (flattened through the role index: inheritance, wildcard, and the `:read` floor
  resolved). The console gains a read-only **Roles** page (`Settings > Roles`): a self-teaching catalog of each
  role's display name, description, inheritance, and effective permissions, ordered by tier, rendered from the
  real seeded roles. The **grant builder** role picker gains a hover tooltip showing a role's description and
  permissions, so an admin sees what a role grants while assigning it. No custom-role CRUD (a later slice; the
  ADR-0013 cover rule must extend to role editing then). Proven by a real-binary API test (owner effective set
  is `*:*`, admin's is broad but not `*:*`, viewer's includes `*:read`, operator inherits the read floor),
  storage round-trip, and web tests (the Roles page renders and tier-orders the seeded roles).
- **Estate slice: per-type location icons.** `location_type` gains an `icon` column (a glyph key,
  seeded `landmark` / `building` / `layers` / `door-open` for the four official types, default
  `map-pin`), projected on `GET /types/location`. The console resolves the key to an SVG and renders
  it as the leading glyph on every location in the tree, tinted the same hue as the type badge, so a
  campus reads differently from a building at a glance. The glyph rides a new reusable `leadIcon` slot
  on the shared `ListView`, and `resolveIcon` falls back to `map-pin` for a key the console does not
  know, so the API can introduce a type icon without a coordinated release. Proven by a storage
  round-trip (the icon survives upsert and idempotent update), a seed test (the four shipped keys), a
  real-binary API test (the icon travels the wire), and a client unit (the resolver and its fallback).
  Deferred: per-location icon override, an operator icon picker (needs a `location_type` write
  surface), and server-side key validation.
- **Dev experience: an example estate on `make dev`.** Distinct from the three production seeding concerns
  the [storage](/architecture/storage/) page names (schema migrations, boot seed, one-time backfills), a
  **dev-only example seed**, applied by `make dev` and never in production. `internal/devseed` embeds a
  fixture (a multi-site estate of three campuses, `hq` / `east` / `airport`, three sign-in-able users, and
  their grants spanning `operator@all`, `viewer@hq` subtree, and `deploy@east` subtree-excl-root) and
  installs it through the [Storage Gateway](/architecture/storage/) on the same trusted direct-DB lane as
  `bootstrap`, exposed as `omniglass seed-dev`. It is idempotent (a re-run of `make dev` changes nothing:
  locations are skipped by name, users by their taken-username error), so a fresh console comes up populated
  instead of empty and nobody hand-creates rows to demo a feature. Proven by a pure fixture-shape unit test
  and a testcontainer integration test (run twice: thirteen locations across three campuses, three users with
  password credentials, three grants of the expected scope, all stable across the second run), plus the
  `seed-dev` command driven end-to-end against a real Postgres. The `/ship-slice` gate now requires any slice that adds a new operator entity to seed an
  example of it here.
- **Identity: the auth event log.** The `audit_log` already recorded every privileged mutation (with `actor`
  and, for impersonated actions, `real_actor`); this adds the read surface and captures auth events. `GET
  /audit-log` (newest first, filter by resource / verb, backward-paged by `before`) resolves each actor and
  real-actor to a username, and **login / logout are now captured** (`resource = 'auth'`, via a non-transactional
  emit seam, since login is a read/no-tx path), and so are **failed sign-ins**: a wrong password on a real
  account is `login_failed` (attributed to that principal) and a correct password against a disabled account is
  `login_denied` (#97), while an attempt on an unknown username is not written (so scanning cannot flood the
  log). The console gains a read-only **Audit** page (`Settings > Audit`): every action and sign-in, with an
  `as <actor>` tag on anything done while impersonating. It is **admin/owner-only**: `audit` is a sensitive
  resource, so a `viewer`'s `*:read` does **not** confer `audit:read`, only an explicit grant or `*:*` (a small
  `rbac` sensitive-resource carve-out, ADR-0014). Proven by an rbac unit test (the carve-out: `*:read` denied,
  `*:*` and explicit allowed), a storage round-trip, a real-binary API test (a login appears, a real-account
  failed login is attributed while an unknown-user attempt is not, a viewer is 403, the resource filter
  narrows, and an impersonated action carries `real_actor`), and web tests (the Audit page renders and marks
  impersonated rows). Deferred: a failed-login rate-limit counter.
- **Identity: permissions are topic patterns.** The capability matcher became a consistent NATS-style topic
  match (ADR-0015, superseding the ADR-0014 carve-out): `*` matches exactly one token and `>` matches the tail,
  so a two-token pattern (`*:read`, `*:*`, `principal:*`) structurally cannot reach a three-token `:admin`
  permission, and admin-sensitivity is a deeper token rather than a special case. `audit:read` became the
  admin-sensitive `audit:read:admin` (the audit route requires it), `owner` became `>`, and the
  `sensitiveResources` set and `grantsAll` helper were deleted. `Set.Allows` matches by token (with the
  unchanged `:read` floor); `Set.Covers` (the impersonation and grant-escalation guard) became pattern
  subsumption plus the floor, staying conservative. The one seed change is `owner`'s `*:*` -> `>`; every other
  permission keeps its meaning (`*` already meant a single token). As a free consequence, `principal:*` no
  longer sweeps a future admin-tier `principal:<action>:admin`. Proven by an rbac unit matrix (`*` one token,
  `>` tail, `*:read`/`*:*`/`principal:*` miss `:admin`, `>` and explicit hit it, Covers subsumption), the whole
  authz suite (the viewer is still 403 on the audit trail, now structurally), and web tests (the Roles view
  renders `>` as an "everything" chip and marks an `:admin` tier). The console Roles view and grant-builder
  render the new grammar. Foundational for a custom-role permission preview (a permission catalog).
- **Identity: the audit page joins the list-view standard.** The Audit page swaps its bare table for the
  console's shared **FilterBar** faceted search (the same one the inventory lists use): filter by `who`,
  `action`, `resource`, or `id`, chips combining to narrow, matched client-side over the loaded rows via the
  `lib/predicate` engine. **Load older** pages backward through the server `before` cursor, so a filter that
  comes up short is a cue to load deeper. Proven by a predicate unit test (each facet, within-chip OR and
  cross-chip AND, the sorted value catalog) and a component test (the FilterBar narrows the rows, and load-older
  asks for events older than the oldest loaded). Deferred: a chip time-range facet (the shared `matchOp`
  compares `gt`/`lt` numerically, so an ISO-timestamp facet needs a date-aware operator in the predicate
  primitive first).
- **Console: the list surface splits into a shell plus swappable bodies.** `ListShell` owns the chrome every
  list wears (the FilterBar chip state and client-side predicate, the card, the error banner, a trailing
  action slot) and hands its body the filtered rows; `FlatList` is the flat body (a sortable table, an
  optional row-to-Drawer detail, an optional create Drawer, an optional footer). The Audit page becomes a thin
  `FlatList` config, behaviour-preserving. This is the primitive-first factoring of the four list idioms that
  had grown up (tree ListView, the Users split-panel, the Roles catalog, the Audit table): the tree pages move
  onto `ListShell` + a `TreeList` body, and Users onto `FlatList`, in follow-up slices; Roles stays the
  read-only catalog until custom-role CRUD. Proven by the Audit component test (unchanged behaviour on the new
  primitives) and the existing web suite.
- **Identity: principal groups (grant-by-group), backend.** A `principal_group` plus membership, and a grant
  that targets a group (a nullable `group_id` on the one `principal_grant` table, exactly-one-of
  principal/group). A member's effective grants are its direct grants unioned with its groups' grants in the
  grant loader, the single seam both the permission flatten and the gateway scope resolution already read, so an
  inherited grant scopes and flattens exactly like a direct one and no other code changes. Group CRUD, member
  add/remove, and group-grant assignment ship on the Gateway and the API (`/principal-groups`), gated by a new
  `principal_group` capability (admin gets `principal_group:*`); a group grant reuses `principal_grant:create`
  and the same escalation cover-check. Proven against real Postgres: a storage integration test (a viewer @ hq
  granted to a group resolves into a member's read scope and permissions; removal and group-deletion drop it;
  dedup / duplicate-name / idempotent-add hold) and an HTTP end-to-end test (a no-access user gains read once
  added to a group holding viewer @ all and loses it on removal; management is `principal_group`-gated; an admin
  cannot group-grant owner).
- **Collection slice (checkpoints 1-2): the storage tier and the node runtime.** The first cut of the
  [collection](/architecture/collection/) engine. Checkpoint 1 landed the storage tier: the `node`,
  `interface_type`, `interface`, `task`, `datapoint_type`, and `metric_datapoint` tables (an idempotent
  dbmate migration), the reachability `datapoint_type` canon and the `icmp`/`tcp` `interface_type`s seeded at
  boot, a scope-safe `metric_datapoint` write path, and the pure reject-not-project registry. Checkpoint 2
  adds the [node](/architecture/nodes/) runtime: `omniglass server` hosts an in-process `nats-server`
  (JetStream enabled) and `omniglass node` enrolls (create, `POST /nodes/{name}:enroll` mints the token,
  `POST /nodes:claim` exchanges it for the NATS credential), then over NATS pulls its worklist
  (`og.v1.worklist.<node>` request-reply, its enabled tasks plus a `config_generation`) and heartbeats
  (`og.v1.heartbeat.<node>`, the server stamps `last_heartbeat_at`). **Per-node subject isolation is real**:
  an in-process auth callback scopes each node's NATS credential to its own subjects, proven by a negative
  integration test (node A cannot publish or pull as node B) against a real embedded server on an ephemeral
  port, plus a fake-seam unit test and the full enroll -> pull -> heartbeat round trip. A node is a
  first-class `principal` of `kind='node'` with a 1:1 `node` detail table (name is the estate address the
  collection FKs reference) and a bearer `credential` row, reusing the human/service identity machinery.
  Deliberate thin cuts ([ADR-0017](/architecture/decisions/)): the credential is an interim shared secret
  stored as a bearer credential (the enrollment token doubles as the NATS password; nkey/JWT and the
  single-use bootstrap deferred), and the control plane is JSON over core NATS. Deferred to later
  checkpoints of this slice: the probes (tcp, then icmp), the protobuf telemetry `Event` over JetStream, the
  ingest consumer, and the API/CLI/UI surfaces (interface and task CRUD, the Nodes inventory and reachability
  panels).
- **Collection slice (checkpoint 3): the reachability datapoint, end to end.** The capability-primitive heart:
  `omniglass node` runs a real **tcp reachability probe** (`collection.NewTCPDialer`, closed over the socket
  boundary and proven by a real-socket integration test, not just a fake seam), ships the result as a protobuf
  `Event` over JetStream (`og.v1.telemetry.<node>`), and the `tcp.open` / `tcp.connect_time` datapoints land in
  `metric_datapoint` owned by the target component. Protobuf is **new** to omniglass: `proto/og/v1/event.proto`
  (`Event` + `Datapoint`, no gRPC service), generated by a `gen-proto` stage on `make gen`. The server hosts an
  `OG_TELEMETRY` JetStream stream and a single durable consumer (`og-telemetry-worker`) that derives inline:
  it binds the owner **server-side** from the task's interface (the node stamps no component identity),
  **confines** a node to its own tasks (an Event carrying another node's `task_id` is orphan-dropped, no row
  written), applies **reject-not-project** (an unregistered datapoint name is dropped), and writes through the
  checkpoint-1 `InsertMetricDatapoints` path (`provenance=observed`). Both invariants are negatively tested
  against real Postgres + a real embedded bus, and the whole path is driven end to end through the real `server`
  and `node` binaries. Deliberate thin cuts ([ADR-0018](/architecture/decisions/)): the hot-path/async split
  collapses (the durable consumer is the at-least-once worklist, no raw-telemetry table or Postgres queue), and
  owner binding is the interface-prebind path only. Deferred: raw-`Event` persistence + replay/backfill,
  label-based multi-owner routing, the remaining probes (snmp, http), and the API/CLI/UI surfaces.
- **Collection slice (checkpoint 4): the icmp (ping) probe, the second capability primitive.** `omniglass node`
  now also runs a real **icmp reachability probe** (`collection.NewICMPPinger`, unprivileged SOCK_DGRAM ICMP via
  `pro-bing`), emitting `icmp.reachable` (always present, `1`/`0` with a `reason` label) and `icmp.rtt_avg` (ms,
  absent when unreachable). It rides the checkpoint-3 pipeline unchanged: the same protobuf `Event`, JetStream
  stream, and owner-confining reject-not-project consumer carry it (the consumer does not branch on probe type).
  The capability question is the point of the primitive: a once-cached loopback self-check tells "this node
  cannot do ICMP at all" (an error, inconclusive, no datapoint) from "this target did not answer" (DATA:
  `icmp.reachable=0` with a down reason), and per the capability-wrapping doctrine a fake-`Pinger` unit test does
  not close the increment, a real-socket integration test (loopback echo + a TEST-NET-1 unreachable address where
  `received==0` is data, not an error) is the closing gate.
- **Collection slice (checkpoint 5a): the reachability verdict as a state.** The first honest substrate for
  availability history. The node now computes the per-interface verdict `interface.reachable` (`up`/`down`, the
  AND of the interface's probe results) and emits it as a built-in **state** datapoint (seeded `datapoint_type`
  at `kind=state`, domain `up`/`down`), riding the proto `string_value`. A new `state_datapoint` table mirrors
  `metric_datapoint` (same owner exclusive-arc, same lineage CHECK) with a categorical `value text` plus optional
  `value_json`, and the Gateway gains `InsertStateDatapoints` / `LatestState` / `StateTransitions`. The ingest
  consumer now **routes by the registry kind** (metric to `metric_datapoint`, state to `state_datapoint`) after
  the **unchanged** owner-confinement and reject-not-project, so a foreign or unregistered state is orphan-dropped
  identically to a metric (negatively tested for the state path). The series is **transition-only**: the node
  remembers the last verdict per interface and emits only on a flip or first observation, and the ingest side
  re-guards by skipping a write whose value equals the latest stored value (the net for a node restart), proven
  by a negative test that repeated identical verdicts produce one row per transition, not per tick. Availability
  as `time_in_state` over this state, and the operator surfaces that render the transitions, are checkpoint 5b.
  See [ADR-0034](/architecture/decisions/#adr-0038-the-reachability-verdict-is-a-built-in-state).
- **Collection slice (checkpoint 5b): the reachability surface, seen.** The first datapoint read surface. A
  per-component read BFF `GET /components/{name}/reachability` (permission `component:read`, scope-injected, so a
  viewer gets a 404 on a component outside its scope) composes, per interface, the latest verdict (`LatestState`
  on `interface.reachable`), the layer signals (`LatestMetric` on `tcp.open` / `icmp.reachable` and their rtt),
  and the verdict's transition history (`StateTransitions` over a window). The operator console gains a
  **Reachability panel** on the component detail: one row per interface with a verdict pill (`responding` /
  `down` / `stale` / `unknown`, staleness derived at read time), an **availability strip** drawn from the state
  transitions (up/down over time, not a latency trend), and an expandable L3/L4 gate breakdown (ping and port,
  the inline probes this slice ships; the L7 app-layer gate lands with the snmp/http/ssh interface types) with
  the reason a down interface is down. It renders only real API fields. The availability-percentage SLI
  (`time_in_state` over the verdict) still rides in with the health slice; the panel shows the transitions
  directly. Interface and task authoring (the "add check" affordance and the flat Nodes / Interfaces / Tasks
  pages) are checkpoints 5c and 5d.
- **Collection slice (checkpoint 5c): the Nodes surface and enrollment.** The first authoring surface for the
  [node](/architecture/nodes/) tier. The console `/nodes` stub becomes a live **Nodes** inventory (the flat
  sibling of the tree lists, a config over the shared `FlatList`): a row per collection daemon with a
  liveness **status pill** derived client-side from `last_heartbeat_at` against the server's down window
  (`OMNIGLASS_NODE_DOWN_AFTER`, 90s: `up` within it, `down` once stale, `never` before first heartbeat), the
  relative last-heartbeat time, and a description, with name and status facets on the same FilterBar the other
  lists use. A row opens a detail Drawer (facts plus an Enroll / Re-enroll action), and **New node** creates a
  node then enrolls it (day-one: create, then `POST /nodes/{name}:enroll`). The minted **enrollment token is a
  secret shown once**: a modal reveals it in a monospace field with a copy-to-clipboard button and a clear
  "shown once, cannot be retrieved again" warning, and the token lives only for the modal's lifetime (cleared
  on close, never written to the query cache, `localStorage`, or a log). The nav tab is gated on `node:read`,
  create on `node:create`, and enroll on `node:enroll` via the same `can()` the sidebar and route guard read;
  the page renders only real `NodeBody` fields. No API or engine change (the node routes already exist), so
  `make gen` shows no drift. Proven by vitest: the data layer (list-envelope unwrap, create body, the `:enroll`
  custom-method POST, the pure status derivation and its window boundary, the status facet catalog) and the
  page (a row per node with the derived pill, the create affordance hidden without `node:create`, and the
  enrollment modal revealing the token, copying it to a faked clipboard, and clearing it on close). The Nodes
  page stays `Partial`: placement, assigned-task worklists, and node health beyond the heartbeat pill are still
  Design. Interfaces and Tasks authoring is checkpoint 5d.
- **Collection slice (checkpoint 5d-api): the interface and task authoring backend.** The operator CRUD over the
  two collection primitives that the reachability read and the node worklist consume. The Storage Gateway gains
  scoped `Create/List/Get/Update/Delete` for both `interface` (name, type, owning component, node placement,
  params) and `task` (a content-addressed id over interface + mode + spec, so identical work dedupes), auditing
  every mutation in-transaction like the estate tiers; the Huma surface adds AIP CRUD at `/interfaces`,
  `/interfaces/{name}`, `/tasks`, and `/tasks/{id}`, regenerated into the OpenAPI document, the cobra CLI
  (`omniglass interface …` / `omniglass task …`), and the typed SPA client. Both authorization layers apply on
  every route: an `interface:<action>` / `task:<action>` **permission** (admin gains `interface:*` / `task:*`,
  operator keeps `create,update`, the `*:read` floor gives everyone read), and **scope** injected by the gateway.
  Neither primitive is a scope-tree entity of its own, so scope **cascades through the owning component**: an
  interface inherits its component's read/action scope, a task inherits its interface's component's, reusing the
  component tier with no new `scope_kind` (an operator scoped to a component subtree administers exactly that
  subtree's interfaces and tasks). A component-less (server-hosted) interface is reachable only under an all
  scope. An interface or task whose component is outside the caller's scope is a **non-disclosing 404** on read
  and update, and a create under an out-of-scope component is a 403, negatively tested at both the gateway and
  the API. No web surface yet: the flat Interfaces / Tasks pages and the "add check" affordance are checkpoint
  5d-ui.
- **Collection slice (checkpoint 5d-ui): the interface and task authoring surfaces.** The console gains the two
  authoring pages the 5d-api CRUD feeds, plus the component-scoped **Add reachability check** affordance. The
  `/interfaces` and `/tasks` stubs become live **Interfaces** and **Tasks** inventories (configs over the shared
  `FlatList`, the flat sibling of the tree lists): Interfaces shows a row per endpoint (name, type, owning
  component, node placement, probed target derived from `params`) with name / type / component facets; Tasks shows
  a row per unit of work (display name or content-addressed id, interface, mode, an enabled pill, node) with
  interface / mode / enabled facets. A row opens a detail Drawer (facts plus an inline edit of the mutable fields
  and a delete), and **New interface** / **New task** open the create Drawer. Because there is no `interface_type`
  list endpoint, the type picker offers the built transports (`icmp`, `tcp`, `ssh`, `http`) this slice ships; a
  future `GET /interface-types` registry route can replace the static options. The **Add reachability check** button
  on the Reachability panel (gated on `interface:create` *and* `task:create`) authors a check the way the node runs
  one: it creates the interface (`type` = the chosen transport, **named by its protocol**, owned by that component,
  `params.target` the `host[:port]` the probe dials) then a **poll** task over it (`mode=poll`, enabled), then invalidates the
  reachability, interfaces, and tasks queries so the panel and pages refresh. The two writes are handled honestly:
  if the task create fails after the interface exists, the affordance surfaces the partial state (the interface is
  created; the operator retries just the task) instead of swallowing it. Every nav tab and action gates on the
  real permission via the same `can()` the sidebar and route guard read (`interface:read` / `task:read` tabs and
  URL guard; create / update / delete actions), and the pages render only real `InterfaceBody` / `TaskBody`
  fields. No API or engine change (5d-api already ships the routes and typed client), so `make gen` shows no
  drift. Proven by vitest: the two data layers (envelope unwrap, create / patch bodies, filter keys, the target
  derivation), both pages (rows, the create gate hidden without the perm, the type / mode / interface pickers),
  and the Add-check affordance (the gate needs both perms, submit fires the interface then the task create with
  the right bodies, and the two-step error path surfaces). This closes the collection slice's authoring surfaces.
  (Superseded before the PR: checkpoint 5f removes the Add-check affordance, and 5g folds the standalone
  Interfaces and Tasks pages into the component and node details and **derives** the task, so these standalone
  surfaces and the `task:create` gate do not ship; the flat pages became detail panels.)
- **Collection slice (checkpoint 5e): the interface model reframe.** Before its PR, the interface model was
  corrected: an **interface is an API we intend to call** (named by the protocol it speaks, `web` / `qrc`,
  unique per component), **not** a network interface; `interface_type` is the **transport** (the reach gate), not
  the protocol driver. Reachability is the first gate of a ladder (reach, auth, responds, collecting). The built
  transports grew from `tcp`/`icmp` to also cover `ssh` and `http` (all reach by opening the tcp port; icmp
  pings), the Add-check picker separates the transport **type** from the protocol **name**, and the dev seed now
  models a lab **polaris DSP** with two protocol-named APIs on one device (`web` over http, `qrc` over tcp) so
  `make dev` shows the "two APIs on one box" story. The driver layer that turns a device API into a normalized
  menu of datapoints and functions (OIDs, commands, parse) is a separate, later concern, decided in
  [ADR-0035](/architecture/decisions/#adr-0039-an-interface-is-a-device-api-the-interface-type-is-its-transport-not-its-driver);
  this slice ships only the transport / reach tier of it.
- **Collection slice (checkpoint 5f): reachability lands read-only; check authoring deferred.** Before the PR, the
  component-scoped **Add check** affordance was **removed**: it was a stopgap that a proper **driver**-based
  authoring flow (attach a driver to a component, fill its inputs) replaces, and the driver/collect layer is
  under active design (template-centric vs driver-centric), decided deliberately rather than on momentum
  ([ADR-0020](/architecture/decisions/) status note). So the reachability panel is now **read-only display**; a
  reachability check is authored by adding an **interface** to its component (checkpoint 5g settles the interface
  as the only authored primitive, its poll task **derived**, and folds the interface and task surfaces into the
  component and node details). The slice lands as **remote node reachability**: enroll a
  node, it probes, the operator sees per-interface reachability. The collect layer (drivers, the normalized menu,
  SNMP) is its own slice after the driver design.
  out of `TreeList` into a standalone `BladeStack` primitive over a `createBladeController`, and both the
  inventory tree and the flat identity pages (`FlatList`) now consume it. A stack entry is a cross-entity
  `{ kind, id }` ref against a registry, so a user's group opens the group's blade over it and a group's member
  opens that user's blade, both stacking. Depth is bounded by construction: each page roots one kind and drills
  one direction (Users: user to group; Groups: group to user; Roles: role), so the drill graph is acyclic and
  the reverse relation on a terminal blade is read-only. A shared `DetailShell` (`Fact`, `RelatedList`,
  `DetailActions`) standardizes the chrome, so a group's members and a user's groups render through one list
  idiom and every footer places the destructive action left of the primary. Roles moves from the card catalog
  onto the same `FlatList` + read-only blade. Behaviour-preserving for the inventory pages (the Locations blade
  is identical); proven by the web suite (the `BladeStack`, `DetailShell`, and both-way identity drill tests)
  and tsc.
- **Console: the read -> Edit -> Save contract on a blade footer action bar.** Every detail blade opens
  read-only; the header is chrome only (back, full-page, close), and a `createEditSlot` per blade drives a sticky
  **footer action bar** that `BladeStack` renders: a destructive action on the left (always available), secondary
  actions in a `⋯` kebab, and Edit on the right (which swaps to Save / Cancel in edit mode). The body reads
  `useBladeEdit().editing()` to switch its sections read-only vs live and registers the whole footer through
  `bind` (`editable`, `save`, `cancel`, `destructive`, `secondary`), so editability and the destructive label
  follow the caller's permission and a read-only blade (a role) registers nothing and shows no bar. In edit mode
  the profile becomes inputs, member add / remove stages locally, and the `GrantBuilder` gains a `bind` so its
  diff commits as part of the one blade Save; the destructive action (Delete for a group, Disable / Enable for a
  user, per the accounts-never-deleted invariant) confirms and is reachable straight from read mode. Save commits
  the staged session together and Cancel reverts. A single consolidated audit event and a config restore point
  (one backend action per Save) remain deferred to a batched endpoint; the user archive / purge lifecycle is
  its own slice. Proven by the web suite (the edit-slot, the `BladeStack` footer-bar chrome, and the Group / User
  read-vs-edit tests) and tsc.
- **Identity: the principal lifecycle (archive + purge), backend.** Beyond the reversible disable, a principal
  can be **archived** (a soft delete, `archived_at`: hidden from the directory, cannot authenticate,
  reversible) and then **purged** (an irreversible hard delete, gated on prior archival and on the
  admin-sensitive `principal:purge:admin`, so admin and owner can purge but a two-token `principal:*` cannot
  reach it). The purge preserves the audit trail: every `audit_log` row now denormalizes the actor's label at
  write time, the audit foreign keys are `ON DELETE SET NULL`, and the read coalesces the live join to the
  snapshot, so a purged actor's history stays legible ([ADR-0016](/architecture/decisions/#adr-0016-a-principal-can-be-purged-and-the-audit-trail-is-denormalized-to-survive-it),
  retiring the "never hard-deleted" rule). The last-active-owner guard extends to archive. Gateway
  (`ArchivePrincipal` / `RestorePrincipal` / `PurgePrincipal`), the `:archive` / `:restore` /
  `:purge` API custom methods, and the generated CLI / client ship; proven against real Postgres by a storage
  integration test (lifecycle + audit-actor survival + owner guard) and an HTTP end-to-end test (the capability
  split, the archive-before-purge gate, soft-delete visibility). The console UI for the lifecycle is a
  follow-up ([#143](https://github.com/hyperscaleav/omniglass/issues/143) backend).
- **Identity: the principal lifecycle in the console, UI.** The user blade's footer action bar presents the
  lifecycle by state: the left slot is the reversible toggle (Disable / Enable, or Restore for an archived
  account), and the kebab holds the escalating red steps (Archive when live, Purge when archived and the
  caller holds `principal:purge:admin`), each confirmed. The detail fetches the principal by id (not the
  directory list, which hides archived) so a just-soft-deleted user stays resolvable, and a **Show
  archived** toggle on the Users directory (a new `include_archived` list param) surfaces hidden accounts
  to re-find one. `BladeSecondary` gains a red tone and the destructive slot a restore tone; the web `can()`
  already honours the three-token `principal:purge:admin`. This slice also **renamed the soft-delete verb
  deactivate to archive** (restore, not reactivate) so the ladder reads pause (disable) to remove (archive)
  to destroy (purge), instead of two pause-synonyms; the column, endpoints, capability, and list param follow.
  Proven by the web suite (the live and archived footer states and the show-archived flow) and a storage test
  for the include-archived list; completes
  [#143](https://github.com/hyperscaleav/omniglass/issues/143) / [#146](https://github.com/hyperscaleav/omniglass/issues/146).
- **Identity: IAM console form + blade hardening, UI.** A polish pass over the identity surfaces. Removing a
  principal now closes its blade cleanly: **archiving**, **purging** (and a group **delete**) closes the blade
  first, drops the dead detail query, then refreshes the list, so the blade never lingers on a stale or 404 view
  of the entity it just removed from the working set.
  **Impersonation** (start and stop) now reboots the console to Home (`window.location`), so the
  permission-driven sidebar and route guard rebuild from the new identity rather than showing the previous
  principal's navigation. The forms gain **inline validation** that mirrors the server's Huma constraints: a
  username and a group name are lowercase handles (pattern `^[a-z0-9][a-z0-9._-]*$`, no capitals or spaces) and
  an email must be well formed (`format: email`), each enforced on the create body server-side and checked in
  the form so an invalid field shows an inline error and disables the submit before the round-trip (a new
  `valid` gate on the blade edit contract disables the footer **Save**). The group create Drawer title now reads
  **New group** (it leaked the `principal_group` resource key), the group create form gained the same
  subtitle the user form has, and the form placeholders are consistent across create and edit. A newly created
  **user or group** opens **directly in edit mode** so its grants (and a group's members, child resources that
  need the parent id) are added in one flow rather than a second step. Proven by an API
  validation test (a malformed username, name, or email is a 422; a valid one is accepted) and web tests (the
  blade closes without an orphan refetch, and an invalid field blocks create and the footer Save).
- **Identity: the Groups console + inherited-vs-direct grants, UI.** A **Groups** page (`Settings > Groups`,
  gated `principal_group:read`) as a config over the shared `FlatList`: list and create, and a row Drawer with
  member add/remove and the grant builder (reused, wired to the group grant endpoints, so a group's members
  inherit what it grants). On the **user** detail, grants now split into **direct** (editable) and **inherited
  from a group** (read-only, tagged `from <group>`), so it is clear where a user's access comes from; the
  principal grant body gained `group_id` / `group_name` for the distinction. Proven by the web suite and tsc,
  with the route guard and sidebar both gating `/groups` on `principal_group`. This completes grant-by-group
  ([#90](https://github.com/hyperscaleav/omniglass/issues/90)); dynamic membership and SCIM group mapping remain
  deferred.
- **Identity: password policy + generator.** A single pure validator (`auth.ValidatePassword`) enforces a
  password policy on the API password surfaces: **create a user** and **self-service change-password**. The
  direct-DB break-glass lanes (`bootstrap`, `set-password`) are exempt (fully trusted, DB-access-gated, the
  recovery path). The policy is **at least 12 characters, not on an embedded common-password denylist, and not
  containing the username** (NIST 800-63B style: length and a blocklist over composition rules). The API bodies
  raise their `minLength` to 12 and map a violation to 422 with a specific message; the console mirrors the
  length and username rules inline (a new `passwordError`) and gates the submit, while the denylist stays
  server-side (a 422 on a manually typed common password). A shared **`PasswordField`** component adds a
  show/hide toggle and, on the New user and change-password forms, a crypto-strong **Generate** action
  (`generatePassword`, a readable charset with no look-alike characters, kept masked and copied or revealed on
  demand) plus a **Copy** button. Proven by an `internal/auth` unit test (short / common / contains-username / valid), an API
  test (a weak, common, or username-containing password is a 422 on create, a strong one is accepted), and web
  tests (the generator's length / charset / policy-compliance, `passwordError`, and the Generate-fills-and-enables
  flow). Completes [#104](https://github.com/hyperscaleav/omniglass/issues/104) and
  [#151](https://github.com/hyperscaleav/omniglass/issues/151); an operator-configurable policy and a HIBP
  breached-password check remain deferred.
- **Identity: admin password reset.** An administrator can reset another user's password without the
  user's current one: `POST /principals/{id}:resetPassword` gated by a new `principal:reset-password`
  capability (all-scope; admin holds it via `principal:*`, so a future help-desk role can be granted only
  the reset), policy-enforced (a 422 on a weak password), refused on self (change your own password from
  your profile, which verifies the current one), behind the same **takeover guard** as
  impersonation (an owner cannot be reset by anyone, and a caller cannot reset a principal whose
  capabilities exceed their own, a shared `checkTakeoverGuard`, plus the shared `allScopeCovers`
  all-scope-only cover that act-as uses so a reset cannot promote a narrow-scope capability estate-wide),
  and audited with the admin as the actor (the
  storage method `SetPrincipalPassword` sets by id, distinct from the self-service `SetPassword` that keys
  on username and audits the target). It also **forces logout**: a reset revokes every one of the target's
  bearer credentials (all sessions and tokens) in the same transaction, so a compromised or departing
  account is cut off at once; the self-service change-password revokes the caller's OTHER sessions
  (`RevokePrincipalBearers` keeping the current session's hash, stashed in the request context by authn). The console adds a **Reset password** kebab action on the user blade
  that opens an inline panel reusing the `PasswordField` (Generate, Copy, inline policy check); the set
  password stays copyable to hand over. This is the console counterpart to the CLI `set-password` (which,
  as a trusted direct-DB lane, stays policy-exempt). Proven by a storage test (the new secret
  authenticates and the old does not, the reset is audited as the admin, non-all scope and unknown id are
  refused), an API test (an operator lacks the capability 403, a weak / common / username-containing
  password is a 422, resetting yourself is a 422 and an owner is a 403, the admin reset lets the user
  sign in with the new password), and web tests (the kebab opens the panel and Generate + Set password
  calls the endpoint and confirms; the action is hidden on your own blade).
- **Identity: bearer session expiry.** Bearer credentials gain a nullable `credential.expires_at` (a new
  additive migration), and `AuthenticateBearer` refuses a credential whose expiry has passed (an expired
  session authenticates nothing). A web login installs its session cookie with a fixed absolute lifetime
  (12h), setting both the credential's `expires_at` and the cookie `Max-Age`, so a stolen session cookie is
  no longer valid forever; the `IssueBearerCredential` gateway method gained an `expiresAt` argument. CLI
  API tokens (`omniglass token`) and the bootstrap token pass a null expiry and do not expire, so
  automation keeps working. Proven by a storage test (a past-expiry bearer is `ErrCredentialNotFound`, a
  future or null one authenticates). A sliding idle timeout and a background sweep of expired rows are
  deferred refinements; the security floor (expired is refused at auth) lands here.

- **Identity: login brute-force lockout.** Failed password logins are now throttled. Two additive columns
  on `human` (`failed_login_count`, `locked_until`) count consecutive misses on a real account and, on the
  5th, lock it for 15 minutes. Inside the window `AuthenticatePassword` refuses every attempt (even the
  correct password) via a new `ErrAccountLocked`, which the login handler maps to the **same generic 401**
  as a bad credential so the lock is not an enumeration oracle (only the `login_locked` audit records it).
  The lock is decided after the argon2 verify (a locked account is not a faster probe), and a correct
  password below the threshold clears the counter. The decision itself is a pure, unit-tested function;
  proven end to end by a storage integration test (5 misses lock, the correct password is refused while
  locked, an expired window lets it through and resets) and a real-endpoint test (the lockout as seen over
  `POST /auth/login`). Per-IP throttling and a configurable threshold are deferred.

- **Identity: force a password change after an admin reset.** An admin reset now sets a new additive
  `human.must_change_password` flag; the user's own change-password clears it. While set, the `authn`
  choke point (the same place view-as read-only is enforced) refuses **every** route with a 403 except
  reading their own principal and the change itself, so the admin-known secret cannot be used to act.
  `GET /auth/me` carries `must_change_password`, and the console's `AuthGuard` swaps the whole app shell
  for a forced change-password screen until it clears (a `useChangePassword` that invalidates `/auth/me`
  releases the gate on success). Proven by a storage test (a reset sets the flag, a self-change clears
  it), a real-endpoint test (a flagged user reads `/auth/me` but every other route is a distinct
  "password change required" 403 until the change, then the gate releases), and a web test (the guard
  renders the forced screen only when flagged). Enforcement is a hard block on all routes; the CLI
  break-glass lanes do not set the flag.

- **Identity: address a principal by username or uuid.** Every `/principals/{id}` route (read, update,
  grants, the lifecycle verbs, reset, impersonate) now accepts either the principal's uuid or a human's
  current username. A new gateway primitive `ResolvePrincipalRef` resolves it (a uuid passes through
  unchanged, so an unknown uuid keeps its existing not-found handling; otherwise a username lookup, and an
  unknown username is a 404), and each handler resolves at the top, before any self-check (so addressing
  yourself by username is caught for a reset or impersonation). The uuid stays the stable identity; a
  username is a convenience address resolved at call time. Service principals have no username and stay
  uuid-addressed. This makes the CLI usable without a uuid lookup first (`omniglass principal archive
  alice`). Proven by a storage test (username resolves, a uuid passes through, an unknown username is not
  found) and an API test (get by username matches get by uuid, a `:verb` method archives by username, an
  unknown username is a 404).
- **Tag: value-domain enums and existing-value autocomplete.** A tag key can now **constrain its values to an enum**,
  answering the enum half of the value-domain open question. A new **`allowed_values`** column on `tag` (empty = free
  text) is the value set a bound value must belong to; `SetTagBinding` enforces it (a non-member is a dedicated 422,
  `ErrTagValueNotAllowed`), so `environment` can be declared as one of `prod` / `staging` / `dev`. The Tags directory
  create and edit forms gain a value-domain control (a checkbox that turns a key into an enum plus a value-list
  editor), and the **TagAdder value stage** renders a **strict dropdown** for an enum key. A **free** key instead
  autocompletes the **distinct values already in use** for it, via a new `GET /tags/{name}:values` read
  (`select distinct value`), so an operator reaches for an existing value without the key declaring a set. Pure
  `web/src/lib/tagdraft` helpers (`isEnumKey`, `valueOptions`, `valueAllowed`) and a pure `internal/tag`
  (`ValidateAllowedValues`, `ValueAllowed`) hold the logic. Proven by unit suites on both pure cores, a storage
  integration test (the enum admits members and rejects non-members, a free key admits anything, narrowing the enum is
  enforced on the next bind, the distinct-values query dedupes and sorts) and an HTTP e2e (the enum 422, the values
  endpoint, the round-trip). A typed `value_type` and input normalization stay deferred ([ADR-0024](/architecture/decisions/#adr-0024-a-tag-key-may-constrain-its-values-to-an-enum)).
- **Tag: apply and remove tags from the entity detail blade (TagAdder).** The capability that closes the
  set-side of the tag console: an operator now binds and unbinds tag values from the UI, not only the CLI. Each
  component, system, and location detail blade carries a **Tags** panel listing the tags bound **directly** on the
  entity as removable colored chips, plus a staged **key -> value** add row. The key stage autocompletes the registry
  filtered by `applies_to` for the entity kind and by what is already bound (a pure `web/src/lib/tagdraft` core:
  `keySuggestions`, `exactKey`, `canCoin`, `valueValid`); with `tag:create`, a **Create key** affordance opens the
  Tags directory's create form ([#192](https://github.com/hyperscaleav/omniglass/issues/192)'s `CreateTagForm`, now
  exported) in a drawer and returns with the minted key selected. Writes are immediate (each is the entity's own
  `:setTag` / `:removeTag` write, gated by its `:update`), so there is no separate Save; the affordances hide without
  the permission. The resolved cascade (inherited tags) stays in the directory [Tags column](/guides/operator/inventory/),
  not the panel. Proven by a `tagdraft` unit suite (applies_to filtering, already-bound exclusion, exact-match and
  coin eligibility, value validity) and a `TagAdder` render test (chips, the update-gated add row and per-chip remove,
  the read-only and empty states). The full winner-plus-shadowed cascade provenance in the blade, the dynamic tag
  search facets, and a stored per-key color override are later slices ([#189](https://github.com/hyperscaleav/omniglass/issues/189)).
- **Tag: the colored effective-tags column on the directories.** The first VISIBLE tag surface, consuming the
  batch resolver: Components, Systems, and Locations each gain a **Tags** column rendering their **effective**
  tags (the resolved cascade winners, not just direct bindings) as `key = value` pills. Color is
  **client-derived** and needs no backend: `tagHue(key)` (a pure `web/src/lib/tagcolor` FNV-1a hash into a
  curated, yellow-green-pruned 12-hue ramp) maps a key to a stable hue, so the same key is the same color
  everywhere; a shared `TagPills` component crosses only that hue into a new unlayered `.tag-pill` CSS recipe
  (text and outline are the seed, the fill is the seed at 15% via `color-mix`), with lightness and chroma in
  per-theme tokens so one hue reads correctly and stays contrast-safe in both themes. The chips stay on one
  line, fading at the edge when they overflow, and a portaled hover tooltip reveals the full wrapped set (a `wrap`
  prop on `TagPills` is the seam for a future per-table wrap toggle). The column registers in each
  page descriptor, so the columns menu shows, hides, and reorders it, and it sorts by key set. A stored per-key
  color override is a later slice; the type-to-add editor and tag search follow ([#189](https://github.com/hyperscaleav/omniglass/issues/189)).
  Proven by a `tagHue` determinism unit test (stable, in-ramp, spread) and a `TagPills` render test (sorted
  chips, the `--tag-h` per key, the empty dash), plus the page-descriptor conformance matrix.
- **Tag: batch effective-tags on the directory rows.** The first slice of the tag-apply UI, backend only: the
  directory list routes (`GET /components`, `/systems`, `/locations`) now carry an **`effective_tags`** map (key to
  winning value, winners only) on each row, resolved for the whole page in one batched query per kind
  (`Gateway.EffectiveTags`), feeding the coming Tags column with zero per-row fetch. Effective resolution extends past
  the component: a **location** resolves the install-wide tier + its own location tree, and a **system** resolves it + its
  system tree + **the location it is placed at** ([ADR-0022](/architecture/decisions/#adr-0022-effective-tags-resolve-onto-systems-and-locations-a-placed-system-inherits-its-location)),
  so a system in a PCI building surfaces `compliance: pci`. Three per-kind recursive-CTE resolvers thread a target id
  through the ancestor chains and rank per (target, key); the resolver is scopeless by contract (the list already
  filtered the ids to the read scope, the rowActions batch pattern). Proven by a storage integration test (the
  four-band component cascade, the placed-system-inherits-location case, the non-propagating flat case, a batched
  shared-ancestor pair with no false cycle, and the empty/unknown edges) and an HTTP e2e (`effective_tags` on the
  component, system, and location list bodies). The Tags column, the type-to-add editor, and tag search follow in
  later slices ([#189](https://github.com/hyperscaleav/omniglass/issues/189)).
- **Tag: the console key directory.** The [tag](/architecture/tags/) vocabulary gets its operator surface: a **Tags**
  directory under Catalog on the shared FlatList shell, where an admin mints a key (`tag:create`), edits its
  governance fields (applies_to and the cascade-versus-flat `propagates` toggle, `tag:update`), and deletes it
  (`tag:delete`, cascading its bindings). Rows address by the key **name** (the write paths key on the name), and the
  create drawer plus the edit blade reuse the estate's drawer and blade primitives, no fork. Binding a value onto an
  entity and the effective-tags cascade panel are the next console slice ([#189](https://github.com/hyperscaleav/omniglass/issues/189)).
  Proven by a data-layer unit test (the envelope unwrap, the create/update/delete request shapes, the error throw)
  and the full web suite, and live-verified against `make dev` (the directory, the create drawer, the edit blade).
- **Tag: the governed label vocabulary and its cascaded bindings.** The [tag](/architecture/tags/) primitive lands as
  its own page: a governed **`tag`** key registry (a normalized lowercase-identifier vocabulary, minting gated by
  all-scope `tag:create`, broadened to `tag:*` for admin), a per-entity **`tag_binding`** cell owned on the same
  exclusive arc as a secret and a variable (`platform | location | system | component`), and a resolver that **unions
  keys and overrides values** most-specific-wins down the [cascade](/architecture/cascade/). The governance split is the
  point: minting a key is admin-curated, but **setting a value is the entity's own write** (`component:update` and
  friends), so an operator tags what it may already edit with no new grant; a platform-tier binding is `tag:update`
  plus `platform:update`. A key
  carries **`applies_to`** (an entity-kind allow-list, checked on bind) and **`propagates`** (cascade inheritance
  versus a flat per-entity set, the shape a [file](/architecture/files/) reuses). `GET|POST /tags`,
  `PATCH|DELETE /tags/{name}`, `POST /tags/{name}:setPlatform|:clearPlatform` (the platform-tier value), and per-entity custom
  methods `GET /{components,systems,locations}/{name}:listTags` and `POST .../{name}:setTag|:removeTag` (bindings are
  entity custom methods, like the principal lifecycle, so the generated CLI stays collision-free), plus
  `GET /components/{name}/effective-tags` (the per-component cascade). Deferred (ADR-0021): the console surface (a Tags
  directory and per-entity editor, [#189](https://github.com/hyperscaleav/omniglass/issues/189)), binding via
  [groups](/architecture/groups/) and a `template`-scoped binding ([#184](https://github.com/hyperscaleav/omniglass/issues/184)),
  value-domain governance ([#190](https://github.com/hyperscaleav/omniglass/issues/190)), and binding onto a file
  ([#191](https://github.com/hyperscaleav/omniglass/issues/191)). Proven by a pure `internal/tag` validation unit test (key
  normalization, value bounds, the applies_to allow-list), a storage integration test (the registry CRUD and all-scope
  gate, the applies_to and value gates, the binding upsert and the owner-update scope split, the full union-on-key /
  override-on-value cascade incl. the non-propagating flat case, and the delete-key cascade to bindings), and an HTTP
  e2e (the cascade over the wire and the mint-versus-bind authz split: an operator binds on its component but cannot
  mint a key nor bind on a system it cannot write, a viewer reads but cannot mint or bind).
- **Variable: the cascade-resolved free value.** The second member of the [config, secrets, and
  variables](/architecture/variables/) trio lands, after the secret. A **variable** is a typed **plaintext**
  value (a macro) owned on the same exclusive arc as a secret (`platform | location | system | component`) and
  resolved most-specific-wins down the [cascade](/architecture/cascade/), but shown in the clear (no crypto, no
  masking, no reveal). Typing is **inline**: a `value_type` of `string` / `int` / `float` / `bool` / `json` plus a
  jsonb `value`, validated against the type in a pure `internal/variable` package (no shape registry, unlike the
  secret). `GET|POST /variables`, `PATCH|DELETE /variables/{id}`, and `GET /components/{name}/effective-variables`
  (the per-component cascade view, **since retired**, [#281](https://github.com/hyperscaleav/omniglass/issues/281)).
  Reads ride the viewer floor; **create and update are granted to operators**
  (`variable:create,update`), delete stays `variable:delete` (admin, owner), the same split the secret got.
  Scoping a variable required wiring `variable` into the ABAC resolver's owner-arc tiers, exactly as the secret
  did. The console adds a **Variables** directory (type in its own column) with a type-aware value editor; a
  per-component **effective-variables** cascade panel also shipped and was **later retired**
  ([#281](https://github.com/hyperscaleav/omniglass/issues/281), the panel-retirement note below). Deferred
  (ADR-0020): the **`template`** owner scope and cascade groups ([#184](https://github.com/hyperscaleav/omniglass/issues/184)),
  a `variable_type` registry (types are inline), the **`$var:`** interpolation consumer, and the secret-flagged
  variable.
  Proven by a `value_type` validation unit test (each scalar's valid and invalid forms), a storage integration test
  (jsonb round-trip per type, full cascade precedence, the owner-scope split), an HTTP e2e (the cascade, the authz
  split incl. the scoped-operator create / update and the viewer 403s), a scope-resolver unit test for the
  owner-arc kinds, and the web data-layer suite.
- **Secret: the cascade-resolved encrypted value.** The first of the [config, credentials, and
  variables](/architecture/variables/) trio lands, and the prerequisite for the [collection](/architecture/collection/)
  driver's interface inputs ([#155](https://github.com/hyperscaleav/omniglass/issues/155)). A **secret** is a
  typed, encrypted-at-rest value owned on the exclusive arc (`platform | location | system | component`) and
  resolved most-specific-wins down the [cascade](/architecture/cascade/). Its shape is a `secret_type` registry
  (per-field secrecy and origin; `snmp-community` and `basic-auth` seeded); crypto is **envelope AES-256-GCM**
  behind a pluggable KEK provider (env / file / fallback), with `(owner, name, field)` bound as AAD so a
  ciphertext cannot be lifted between rows. The load-bearing new piece is the **cascade resolver**: it walks the
  three owner trees up from a component, tags each owner with a band and depth, and ranks per name (highest tier,
  then deepest), returning the winner and the shadowed candidates. `GET /types/secret`, `/secrets` (all-scope
  list, create, update, delete), `GET /components/{name}/effective-secrets` (the masked cascade view, **since
  retired**, [#281](https://github.com/hyperscaleav/omniglass/issues/281)), and the
  audited decrypts `POST /secrets/{id}:reveal` and `:copy` (a clipboard copy, recorded under a distinct `copy`
  verb). Masked reads ride the viewer floor; **create and update** are gated by `secret:create` / `secret:update`
  and granted to **operators** in their scope; **delete** stays `secret:delete` (admin, owner); **reveal** and
  **copy** by the sensitive `secret:reveal`, which the `*:read` floor does not carry, so only admin (`secret:*`)
  and owner may decrypt. Scoping a secret required wiring `secret` into the ABAC resolver's owner-arc tiers
  (location / system / component), so a scoped grant now confers secret scope over its subtree (before, only an
  all-scope owner could). The console adds a **Secrets** directory (type in its own column) with per-field
  in-place reveal + copy adornments; a per-component **effective-secrets** list also shipped (each resolved
  secret opening a blade that decrypts the value and shows the full cascade top-to-bottom) and was **later
  retired** ([#281](https://github.com/hyperscaleav/omniglass/issues/281), the panel-retirement note below).
  Renamed **credential to secret** (ADR-0017).
  Proven by a real-crypto testcontainer test (seal, jsonb round-trip, unseal; encrypted-at-rest; full cascade
  precedence), an HTTP e2e (the cascade, the authz split incl. the scoped-operator create / update, and the
  reveal / copy round-trips plus their 403s), a scope-resolver unit test for the owner-arc kinds, and the web
  data-layer suite. Deferred: the **variable** and **config** members of the page, the interpolation consumer,
  secret **lifecycle** (oauth2 refresh, rotation), and groups / weighted precedence in the cascade.

- **Identity: clear the login lockout on a password rotation.** Rotating a password now clears the
  brute-force lockout (`failed_login_count = 0`, `locked_until = null`) in the same transaction as the new
  secret. Before this the lock (from the login-throttle slice) only cleared lazily at the next login, so an
  admin reset left the account locked for the rest of the 15-minute window even with the new password. Both
  rotation lanes clear it: the admin reset (`SetPrincipalPassword`, the reachable intervention while an
  account is locked) and the self-service change / CLI set-password (`SetPassword`). No new endpoint or
  capability: rotating the secret is the intervention (auto-unlock over a dedicated `:unlock` action).
  Proven by a storage test that locks the account with five misses, confirms it is locked, rotates via each
  lane, and asserts the new secret authenticates immediately with no wait. No migration (the columns exist);
  no operator-facing surface change.

- **Identity: profile pictures for human principals.** A human principal can carry a **profile picture**,
  managed on two lanes: **self** (any signed-in user sets or removes their own via `POST /auth/me:setAvatar`
  / `:removeAvatar`, authn-only and self-scoped, no capability, on the ungated self-service lane) and
  **admin** (a new `principal:set-avatar` all-scope capability sets or removes any principal's via
  `POST /principals/{id}:setAvatar` / `:removeAvatar`, audited with the admin as actor; `admin` holds it
  through `principal:*` and `owner` through `>`, so no `roles.yaml` change). A pure `internal/avatar.Normalize`
  primitive is the server-authoritative pipeline: accept JPEG/PNG/WebP (GIF and all else rejected), reject a
  payload over 8 MiB or any dimension over 8000px (decompression-bomb guards), center-crop to a square, resize
  256x256, re-encode JPEG q82; a bad or oversize image is a 422. Two additive columns on `human` (`avatar`
  base64, `avatar_updated_at`) store the one normalized size; the bytes never load on the `loadPrincipal` hot
  path (which selects only `avatar is not null`), so a cheap `has_avatar` bool rides the read models
  (`GET /principals/{id}`, the Users list, `GET /auth/me`). The **read endpoint is JSON**
  (`GET /principals/{id}/avatar` gated `principal:read`, `GET /auth/me/avatar` self), returning
  `{ image_base64 }` the console renders as a `data:` URL, deliberately not a raw `image/jpeg` handler so every
  route stays under the Huma authz middleware
  ([ADR-0018](/architecture/decisions/#adr-0018-the-avatar-read-endpoint-is-json-not-raw-image-bytes)); a
  principal without a picture is a 404. The console **Profile** page gains an image-backed avatar with
  upload/remove (initials fallback), and the **Users** directory renders per-row thumbnails plus an admin
  upload/remove panel gated on `principal:set-avatar`. The CLI and typed client fall out of the generator
  (`omniglass principal setAvatar` / `removeAvatar`). Proven by the `internal/avatar` golden-fixture unit
  suite (a square JPEG out, non-image / GIF / oversize rejected), a storage round-trip (set / get / clear,
  `has_avatar` and `avatar_updated_at`, the all-scope gate on the admin write), and a real-binary API test
  (self set + read + remove, garbage 422, the admin permission 403 without `principal:set-avatar` and success
  with), plus web tests for both surfaces. Deferred: the top-nav avatar, non-human avatars, multiple sizes /
  srcset, and external (Gravatar) sources.

- **Identity: view and revoke your own sessions (slice 1 of 2).** A signed-in user can now see and end
  their own sign-ins and API tokens. Two gateway primitives land on the existing `credential` table (no
  migration; `created_at` and `expires_at` already exist): `ListBearerCredentials` returns each of a
  principal's bearer credentials with only non-secret metadata (id, `ogp_` prefix, created, expiry) and
  never selects the raw `secret_hash` into a returned field (the request's own `sha256(token)` is compared
  in SQL to flag the `current` one, so the hash never leaves the database), and `RevokeBearerByID` deletes
  one **scoped to the owning principal** so a caller can only revoke their own. Over the API, `GET
  /auth/me/sessions` lists them (labelled `session` or `token` by the credential's `purpose`, the
  current one flagged) and `POST /auth/me/sessions/{id}:revoke` ends one (204); both are authn-only and
  self-scoped, so a credential id belonging to another principal is a non-disclosing 404, and revoking the
  current credential signs it out. The console's **Your profile** gains a **Sessions** card listing each
  credential with a **Revoke** action (**Sign out** on the current one), through the generated typed client
  and TanStack Query. The generated CLI gains `omniglass session list` / `session revoke <id>`. Proven by a
  storage test (list returns both with metadata and no secret and the current flag, revoke by id scoped to
  the principal, a cross-principal or malformed id is a no-op) and a real-binary API test (the current
  session is flagged, a second session is revoked and stops authenticating, another principal's id is a
  404, and revoking the current one signs out). The **admin** surface (an operator revoking another
  principal's sessions) is slice 2.

- **Identity: every credential is time-bounded, and sessions split from API tokens.** Building on the slice
  above, the tokens-never-expire behavior is reversed ([ADR-0019](/architecture/decisions/#adr-0019-every-credential-is-time-bounded-token-purpose-not-expiry-shape)):
  a web-login **session** keeps its 12h absolute lifetime, while a CLI/API **token** (`omniglass token`) and
  the **bootstrap token** (`omniglass bootstrap`) now get a **90-day default** expiry with a `--ttl` override
  hard-capped at **365 days** (a `--ttl` above the cap is a clean error, before any DB work), so no eternal
  secret sits in the field. Because both kinds now carry an expiry, a new **`credential.purpose`** column
  (`session` / `token`), not the nullable `expires_at`, is the discriminator; a migration adds it and
  backfills existing bearers (expiry set to `session`, else `token`). `IssueBearerCredential` and
  `BootstrapOwner` take the purpose and expiry; `ListBearerCredentials` returns the purpose and now filters
  to **live** rows only (`expires_at is null or expires_at > now()`, mirroring `AuthenticateBearer`), so a
  dead credential is never listed. The console's **Your profile** splits the one list into a **Sessions**
  section and an **API tokens** section, both rendering a shared `SessionsList` primitive, and a token now
  shows its expiry. Enforcement stays **lazy** (an expired row is refused at auth, no background sweep).
  Proven by a storage test (the list carries the right purpose and excludes an expired row), an updated
  real-binary API test (session vs token asserted via purpose, the minted token carries a future expiry), and
  a CLI unit test (a `--ttl` above the 365-day cap errors for both `token` and `bootstrap`). Deferred: a
  sliding idle timeout, a housekeeping sweep of long-expired rows, and nearing-expiry notifications.

- **Identity: an admin can view and revoke a user's sessions (slice 2 of 2).** An administrator can now
  see and end **another** principal's sign-ins and API tokens, so a lost laptop or a leaked token is cut
  off without resetting the account. No new storage: it reuses slice 1's `ListBearerCredentials` (passing a
  nil `currentHash`, so no row is ever flagged `current` when viewing someone else) and the
  principal-scoped `RevokeBearerByID`. A new **`principal:revoke-session`** capability (a normal two-token
  permission, held by `admin` and `owner` through their `principal:*` / `>` wildcards, kept separable for a
  future help-desk role) gates both `GET /principals/{id}/sessions` and
  `POST /principals/{id}/sessions/{sid}:revoke`. The revoke is bounded to the target (a `sid` that is not
  theirs is a non-disclosing 404), sits behind the same **takeover guard** as impersonation and the
  password reset (an owner's sessions cannot be revoked by a lesser admin, nor a principal whose
  capabilities exceed the caller's), and is **audited with the acting admin as the actor**. The console's
  **Users** blade gains **Sessions** and **API tokens** sections (a shared `SessionsList` now backs both
  them and the self-service Profile card), hidden unless the caller holds the capability, with a **Revoke** per row.
  The blade's action-rail kebab also offers **Revoke all sessions** and **Revoke all tokens** (each confirmed),
  bulk-ending one purpose at a time through a new `POST /principals/{id}/sessions:revokeAll` backed by a
  purpose-filtered `RevokeBearersByPurpose` (revoking sessions never touches tokens), returning the count and under
  the same gate, takeover guard, and audit as the single revoke. The generated CLI gains
  `omniglass principal sessions <id>` / `principal revoke-session <id> <sid>` /
  `principal revoke-all-sessions <id>` (the cligen `nameOverride` seam groups them under `principal` so they do
  not collide with the self-service `session` commands, which share the leaf noun). Proven by real-binary API
  tests: an admin lists a target's sessions and revokes one so it stops authenticating, bulk-revokes all sessions
  (both cookies stop authenticating, the tokens survive) then all tokens, an operator is 403, revoking an
  **owner's** session or bulk-revoking its tokens as a lesser admin is 403 (the takeover guard), a `sid` that is
  not the target's (or an unknown principal) is a 404, a bad purpose is a 422, the secret never appears, and the
  revoke is audited with the admin as actor.

- **Identity: a password change keeps API tokens, and self-service bulk revoke.** Two refinements to the
  session/token model. First, a password change now revokes **sessions only, never tokens** ([#194](https://github.com/hyperscaleav/omniglass/issues/194)): a token is its own bearer secret, not tied to the password, and has its own revoke surface, so both the admin reset (`SetPrincipalPassword`, now `purpose = 'session'`) and the self-service change (`RevokeBearersByPurposeExcept(pr, "session", keep)`, keeping the current session) leave the target's tokens intact. Second, the self-service **Profile** gains **Revoke all** on each of its Sessions and API tokens sections ([#195](https://github.com/hyperscaleav/omniglass/issues/195)), through a new authn-only, self-scoped `POST /auth/me/sessions:revokeAll` (a `{ purpose }` body) that ends all of one kind at once but **always keeps the credential making the request**, so a user is never signed out of the one they are on; the generated CLI gains `omniglass session revoke-all`. Both are built on one shared primitive, `RevokeBearersByPurposeExcept` (the plain `RevokeBearersByPurpose` becomes its nil-keep case, and the now-unused `RevokePrincipalBearers` is retired). The admin blade also stops offering revoke on an **owner** target (whom the takeover guard makes un-revocable): its session/token list renders read-only, with a line explaining an owner can be seen but not ended. Proven by a storage test (the keep-current filter revokes the others and keeps the current + the tokens), an updated real-binary API test (a password change and an admin reset both leave the token authenticating while the sessions die), a new self-service bulk-revoke API test (all sessions except current, then all tokens, a bad purpose 422), and web tests (the Profile Revoke all posts the purpose, and an owner target hides every revoke).

- **Identity: break-glass `set-password` locks out live sessions.** The direct-DB `omniglass set-password <user> <pw>` (the trusted recovery lane, and the **only** way to touch a compromised **owner**, whose credentials the API guards leave un-revocable) previously rotated only the password, so a stolen session cookie or API token kept working after the reset ([#198](https://github.com/hyperscaleav/omniglass/issues/198)). It now also revokes the target's live **sessions** (a break-glass is a lockout), and revokes its API **tokens** too with `--revoke-tokens` (kept by default, matching the password-change-keeps-tokens rule; a full compromise wants the flag). It reuses the `RevokeBearersByPurpose` primitive after `SetPassword`, resolving the principal by username; no API or console change (break-glass stays behind Postgres access). Proven by a real-Postgres CLI test: a live session stops authenticating after the reset, the token survives without the flag and is revoked with it, and the new password authenticates while the old one does not.

- **Identity: self-service tokens, token descriptions, and session identification.** Three coupled additions to the bearer-credential model. A signed-in user can **mint its own** API token from the console (a **Create token** action on the Profile API tokens section) or the API (`POST /auth/me/tokens`, authn-only and self-scoped): a **required description** and an optional `ttl_days` (default 90, capped at 365), returning the secret **once** ([#204](https://github.com/hyperscaleav/omniglass/issues/204), [#205](https://github.com/hyperscaleav/omniglass/issues/205)). A token now **must** carry a description (the CLI `omniglass token` gains a required `--description`, the bootstrap token gets a default, a session leaves it empty), so a user can tell tokens apart. And every credential records the **device and address** that created it: three additive `credential` columns (`description`, `user_agent`, `client_ip`), captured by a middleware before Huma so both login and the token mint stamp them, plus the existing `last_used_at` now **bumped on authentication** (throttled to the minute). The session and token lists (self and admin) show a **device label** (a coarse User-Agent parse, `Chrome on macOS`; `CLI / API` for a token), the creating **IP**, and a **last active** time; `IssueBearerCredential` is struct-ified to a `BearerIssue` to carry the metadata. Location from the IP (a GeoIP lookup, [#193](https://github.com/hyperscaleav/omniglass/issues/193)) is deferred: v1 is IP and User-Agent only. Proven by a storage test (the identity fields round-trip and `last_used_at` bumps on auth), a real-binary API test (self-mint returns the token once, it authenticates and lists as a described token, a blank description or over-cap ttl is a 422), a `deviceLabel` unit test, and a Profile web test.
- **Identity: the Users, Roles, and Groups directories move to the admin tier.** The directory reads of
  `principal` (list, get, and the profile-picture route), `role`, and `principal_group` are promoted from a
  two-token `<resource>:read` to the admin-sensitive **`<resource>:read:admin`**, so the `viewer` read floor
  (`*:read`) no longer reaches the Users, Roles, and Groups pages, on the console or the API. This supersedes
  the earlier `role:read` / `principal_group:read` / `principal:read` gates named in the slices above. `admin`
  carries an explicit `<resource>:read:admin` alongside its wildcards, the same shape as `principal:purge:admin`;
  `owner`'s `>` is unaffected, and create, update, and the lifecycle verbs stay two-token. Proven by a
  real-binary API test (a viewer@all 403s on `/principals`, `/roles`, `/principal-groups`; admin and owner
  200), the nav filter's web test (the three tabs hidden from `*:read`, shown to admin), and rbac matcher
  cases ([ADR-0023](/architecture/decisions/#adr-0023-the-iam-directory-reads-principal-role-principal_group-are-admin-tier)).
  Secrets, which an operator legitimately reads in scope, are handled by a later slice.
- **Secret: admin-sensitivity and a scoped, sensitive-off-the-floor directory.** The second half of the
  visibility rework, for secrets. Two axes now decide who reaches a secret ([#210](https://github.com/hyperscaleav/omniglass/issues/210)):
  **placement scope** (unchanged) gives locality, and a new per-secret **`admin_sensitive`** column flips a
  secret to the **`:admin` tier**, so a platform credential (a Zoom or Microsoft client secret) stays
  admin/owner-only even at the same scope as an operational device secret. The type carries a
  **`default_admin_sensitive`** that seeds the create form (a new `oauth2-client` type defaults sensitive,
  `snmp-community`/`basic-auth` operational). `secret` also joins a **sensitive-resource set** a bare `*`
  does not reach, in both the direct match and the `:read` floor (Go rbac and the client `can()`), so a
  `viewer` (only `*:read`) reads no secrets while `operator`/`deploy` gain a scoped
  `secret:read,reveal,create,update`; `admin`'s `secret:*` becomes `secret:>`. Enforcement is a `canAdmin`
  capability computed at the API and passed to the Storage Gateway: the `/secrets` directory is now
  **scope-filtered** and hides admin-sensitive rows, and reveal/update/delete of a hidden secret is a
  **non-disclosing 404**; creating an admin-sensitive secret needs the admin tier. Proven by a real-binary
  API test (an operator sees and reveals its in-scope device secret but a non-disclosing 404 on the
  admin-sensitive one and an out-of-scope one, and is 403 creating a sensitive one; a same-scope admin and
  the owner see and reveal it), storage integration tests (the scoped list, the seeded type defaults), rbac
  matcher and covers cases, and the nav/`can()` web tests (Secrets hidden from `*:read`, shown to an explicit
  `secret:read`). The move of Secrets, Variables, and Config into Catalog is a separate branch
  ([ADR-0025](/architecture/decisions/#adr-0025-secret-is-a-sensitive-resource-a-per-secret-admin_sensitive-flag-flips-a-secret-to-the-admin-tier)).
- **Tag: filter the directories by their effective tags.** The last major tag piece, closing the
  set-and-see loop's read half: the Components, Systems, and Locations chip filters gain a single **tag**
  field that discloses one facet per tag key in use, so an operator narrows a directory by any tag through
  one guided step (**tag**, then the key, then the value) rather than a top-level field per key. A tag facet
  reads a row's **effective** value (a component matches on a tag it inherits from its system or location,
  not only a direct binding), autocompletes the values already in use for that key, and offers two new
  **value-less** operators, **is set** (`?`) and **is absent** (`!?`), that test only whether the tag is
  present. These land in the shared [`lib/predicate`](/guides/operator/inventory/#filter) engine (an
  `exists` / `absent` `OpKey` carrying a `valueless` flag, threaded through `opsFor`, `matchOp`,
  `buildPredicate`, and `tokenToChip`) so every FilterBar inherits them, plus a `tagFilterKeys` helper that
  projects one `FilterKey` per tag key present on the loaded rows; the FilterBar keeps those presence facets
  out of the top-level field list and groups them under the `tag` entry (a direct `key:` still works as a
  fast path). `ListConfig.filterKeys` becomes **accessor-reactive** (a `FilterKeys<T>` is an array **or** an
  accessor, resolved in a reactive scope), so the facet set appears the moment the effective tags resolve and
  tracks whatever the estate is tagged with; each page merges its static facets with a `tagFacets` memo
  derived from the rows. Deferred: server-side tag filtering at pagination scale (today the match is
  client-side over the loaded rows) and a stored per-key color override
  ([#226](https://github.com/hyperscaleav/omniglass/issues/226)). Proven by a `lib/predicate` unit suite (the
  presence operators, the per-key facet projection and its dedup against the static keys, the value-less
  tokenizing) and `FilterBar` render tests (the `tag` group discloses its keys, and a `?` / `!?` token
  commits a value-less chip that renders with no value button).
- **Estate: the `type` capability resource gates the location/system/component type registries.** The three
  classifier registries (`location_type`, `system_type`, `component_type`) gain full CRUD behind one new,
  capability-only, unscoped **`type`** resource, replacing the borrowed `location:read` gate on
  `list-location-types`. `type:read` needs no new grant (already covered by `viewer`'s `*:read` floor);
  `type:create,update,delete` is granted to `admin`. A shared storage primitive (`typeregistry.go`) carries the
  sentinels and the delete guard: an `official` (seed-owned) row is read-only (422 on update/delete), and a row
  still referenced by a location, system, or component is refused on delete (409, a Gateway pre-count backstopped
  by the parent foreign key's `NO ACTION`). `system_type` and `component_type` also gain their first `list` route
  (previously list-only for `location_type`, absent for the other two), retiring the console's fake-from-existing-
  rows type picker workaround. `secret_type` is untouched (list-only; its fields-schema editor is a separate
  follow-up). Proven by a storage round-trip per registry (create, duplicate id 409, update, official read-only,
  in-use delete refused) and a real-binary API test per registry (the CRUD lifecycle, 422 official, 409 in-use,
  auto-discovered 403 without `type:*` by the route-gating guard). The generated CLI and web client ship in the
  same slice (`make gen`). Deferred: the Types catalog console page (segmented tab per kind, CRUD drawer
  over the three writable kinds plus a read-only view of `secret_type`) is a follow-up slice.
- **Console: nav IA rework, estate values get their own group and Settings becomes Admin.** The
  sidebar's `nav.ts` gains a new **Values** top-level group (Variables, Secrets, Config), the
  estate-attached values that used to sit under Settings, standing beside **Inventory** (Components,
  Systems, Locations, and **Nodes**, the collection daemons) rather than nested inside it. Interfaces
  and Tasks are dropped from the nav: an interface is a panel on a component and a task is a panel on
  a node, both future work, not directories of their own. The Settings group is renamed **Admin**
  (Users, Roles, Groups, Audit) and gains an ungated **soon** Settings leaf reserving the
  platform-preferences page (severity scales, schedules, retention, defaults)
  ([#222](https://github.com/hyperscaleav/omniglass/issues/222)). Routes stay flat and unchanged
  (`/secrets`, `/variables`, `/config`, `/nodes`, the new `/settings` stub), so only the grouping and
  labels move; this supersedes the earlier same-day plan to move those values into Catalog
  ([decision log](/architecture/decisions/)). Proven by `nav.test.ts` (the Inventory and Values group
  order and labels, Admin's renamed label and its soon-stub surviving for both an owner and a bare
  `*:read` viewer, the moved entries keeping their existing gates) and a `Sidebar.test.tsx` render
  test (the Inventory and Values groups actually render in the expanded submenu).
- **Console: the Types catalog page.** The UI follow-up to the type registry CRUD API above:
  **[Catalog > Types](/guides/admin/types/)** (`/types`) is a **segmented tab control** over all four
  classifier registries (`location_type`, `system_type`, `component_type`, `secret_type`), the first catalog
  page to span several registries in one page rather than a directory per primitive. Each tab (Location,
  System, Component, Secret) renders its own `FlatList` directory scoped to that kind's rows, so the tab is
  the facet: no cross-kind table and no kind filter to type in. Name and official/custom still narrow the
  active tab, and rows are addressed `<kind>:<id>` on the shared blade stack since an id is unique only
  within its own kind. The create drawer opens scoped to the active tab when it is a writable kind (location,
  system, component); an **official** (seed-owned) row and every row on the Secret tab render with neither
  Edit nor Delete, the Secret tab instead showing each row's declared fields (name, scalar type, secrecy,
  origin) read-only, with a note that the fields-schema editor is a follow-up. The page reuses the shell
  built for the identity surfaces (`FlatList`, `BladeStack`, the blade edit contract) rather than a bespoke
  layout, so a fifth classifier registry is a `TYPE_KINDS` entry and a route, not a new page shape. Proven by
  a web data-layer unit suite (`lib/types.ts`): the four-registry aggregation tags each row's kind, a
  location row carries its icon and a secret row its fields, and create/update/delete each refuse the
  `secret` kind without a network call. Deferred: the `secret_type` fields-schema editor (noted above) and a
  page-level component test of the blade CRUD flow.
- **Inventory: create-as-route and the read-only view invariant.** Creating a component, system, or location no
  longer returns you to the list. `New` navigates to `/<entity>/create`, a **draft accordion** (Identity and
  Placement writable, the Tags section locked until the entity exists); **Save** commits the row and hands off to
  `/<entity>/<id>` in **edit mode** via a one-shot pending-edit flag, so you tag and finish configuring in place.
  The detail is one accordion, **read-only in view and the sole writer in edit**: the own-field inputs and the
  `TagAdder` write controls appear only in edit (the footer Edit/Delete and the read-only effective-secrets/variables
  panels, both since retired ([#281](https://github.com/hyperscaleav/omniglass/issues/281)), were exempt), which
  retires the old drawer-reopen edit and the mutation-in-view. This is the Users
  inline-blade-edit model generalised to inventory, on both the docked blade and the full page
  ([ADR-0027](/architecture/decisions/#adr-0027-create-is-a-route-inventory-create-and-edit-unify-on-the-detail-accordion),
  [#231](https://github.com/hyperscaleav/omniglass/issues/231)). The shared `TreeList` gains a per-surface edit slot
  on `ListCtx` (the full page makes its own, since the shared `renderDetail` must not call `useBladeEdit` outside a
  blade provider), plus `renderCreate` / `onNew` / `onEdit` hooks and an optional `FormBody`. Proven by the live
  console (draft to create to edit hand-off, then a read-only fresh visit) and per-page web tests for the three
  entities (the draft route renders, the view has no tag-add control). Deferred: a shared cross-page form shell
  (slice 2) and moving the Users page onto it (slice 3).
- **Estate: `rank` retired from the type registries; alphabetical sort.** `rank` was sort-only
  (no nesting enforcement, per its own seed comment) on `location_type`, `system_type`, and
  `component_type`; a new dbmate migration (`ALTER TABLE ... DROP COLUMN IF EXISTS rank`,
  idempotent) drops it from all three tables, and `ListLocationTypes`/`ListSystemTypes`/
  `ListComponentTypes` now `ORDER BY display_name, id`. The field is gone from the three type
  bodies, the boot-seed YAMLs, the generated client and CLI, and the
  [Types catalog](/guides/admin/types/) page (no Rank column, no Rank field on create or edit).
  This is the mechanical precursor to `allowed_parent_types` (placement constraints on
  `location_type`), which lands as its own slice
  ([#239](https://github.com/hyperscaleav/omniglass/issues/239)). Proven by the storage
  round-trip per registry (alphabetical order, not insertion or id order), the boot-seed
  idempotency test (the lowest-alphabetical official row per registry), and a web test suite
  update (no Rank fixtures, no Rank assertions).
- **Estate: technical-name rename with an inline name check.** A component, system, or location's
  technical name (its `name`, the URL address) is now editable from the detail accordion in edit
  mode rather than fixed at creation. Because `id` (uuidv7) is the identity and every foreign key,
  tag/variable/secret binding, and placement references that UUID, a rename is a one-column update
  with no cascade. A shared `ValidateEntityName` slug rule (`^[a-z0-9][a-z0-9-]*$`) is the server
  source of truth, mirrored client-side, and now gates create as well as rename. An inline **Check**
  button calls a collection-level `POST /<entity>:checkName` that reports `{valid, available,
  reason}`; availability is deliberately **scope-blind** to match the global unique constraint (a
  scope-aware check would false-positive on a name held outside the caller's scope) and is gated by
  `<entity>:update`. The check is advisory: Save stays enabled and the unique constraint (409) is the
  real gate. `make gen` carries the new field and method through to the typed client and CLI. Proven
  by the rename round-trip per entity (a child or binding's UUID foreign key still resolves after the
  parent renames, and the old name frees for reuse), dup-name 409, bad-slug 422 at both create and
  rename, the three checkName states, and a scope-blind test (a `deploy` principal sees an
  out-of-scope name as taken, not available), plus per-page web tests for the editable field and the
  read-only view ([#245](https://github.com/hyperscaleav/omniglass/issues/245)). Deferred: an
  old-name to id redirect for bookmarked links (a soft 404 today), and wiring the check into the
  create draft's live validation.
- **Files: the content-addressed blob store and the file handle.** The first
  [files](/architecture/files/) slice. A **`blob`** store lands as a Storage Gateway **primitive**: a
  `blob.Store` seam with a default **pgblobs** backend (bytes held inline in Postgres), keyed by the **sha256**
  of the bytes so identical bytes **dedup** to one row (`on conflict do nothing`), integrity-verified on read.
  On top of it, the **`file`** handle, searchable metadata (name, content_type, size, sha256, sensitive) that
  points at a blob by hash, gets CRUD over the API: **create-from-upload** hashes and dedups server-side, plus
  get, list, **download** (bytes read back and hash-verified), and delete (which **frees the blob** in the same
  transaction when no other handle still references it, dedup-aware, so storage is reclaimed; async mark-sweep GC of
  aged/event-referenced blobs is a later slice). Access is two layers with **no new machinery**: the `file:<action>` permission, and a per-file
  **`sensitive`** flag reusing the secret `:admin`-tier rule (a flagged file is hidden from a non-admin lister
  and a non-disclosing 404 to a non-admin reader, admin-only to create), defaulting **off** and leaving `file`
  off the sensitive-resource set so the viewer floor reads ordinary files. Files carry **no placement scope** (a
  file is tenant-wide; its 1:many locality is a future attachment, not an owner arc). The generated CLI (a `file`
  command group) and typed client follow from the Huma structs, and the **Files directory** ships under Values
  (upload, download, delete, a sensitive badge). Proven by the pure blob and file-validation unit tests, the
  **pgblobs testcontainer** round-trip and dedup gate (the capability-wrapping close), the gateway integration
  tests (round-trip, dedup, delete-leaves-blob, the sensitive gate), and a real-binary API e2e (upload to
  download identical bytes, the viewer/operator/admin permission and sensitive gates)
  ([ADR-0029](/architecture/decisions/#adr-0029-files-slice-1-a-content-addressed-blob-store-and-a-tenant-wide-file-handle),
  [#242](https://github.com/hyperscaleav/omniglass/issues/242), [#244](https://github.com/hyperscaleav/omniglass/issues/244)).
  Deferred: attach-to-entities-and-types (slice 2), tags-on-files ([#191](https://github.com/hyperscaleav/omniglass/issues/191)),
  async mark-sweep GC of aged/event-referenced blobs, the S3 and disk backends, and the classification lattice
  ([#243](https://github.com/hyperscaleav/omniglass/issues/243)).
- **Estate: `allowed_parent_types` constrains where a location may be placed.** `location_type`
  gains `allowed_parent_types` (`text[]`, default `{}`, a new idempotent migration): a set of
  `location_type` ids and/or the reserved `root` sentinel a location of that type may sit under.
  Empty is unconstrained (every existing custom type, unchanged); non-empty is enforced on
  `CreateLocation` and a new move primitive on `UpdateLocation` (a `parent` patch field,
  cycle-guarded). A violation is a distinct `storage.PlacementError` (wraps
  `storage.ErrPlacementNotAllowed`, carries the offending child and parent type names), mapped to
  a 422 that names both. `CreateLocationType` refuses the id `root`, so a real type can never
  collide with the sentinel. The four seeded types ship their sets: `campus={root}`,
  `building={root,campus}`, `floor={building,campus}`, `room={floor,building,campus}`. The
  [Types catalog](/guides/admin/types/) gains an allowed-parents editor (a checkbox list over the
  location types plus a Root option) on the location tab's create and edit forms, and the
  **location edit form's Placement section makes Parent editable**: a picker (riding #240's
  inventory edit model, the same `Show when={editing()}` split as every other editable field)
  narrowed to the type's allowed parents and excluding the location's own subtree, wired to the
  move primitive; moving back to root is not offered
  ([ADR-0030](/architecture/decisions/#adr-0030-allowed_parent_types-constrains-where-a-location-may-be-placed),
  [#239](https://github.com/hyperscaleav/omniglass/issues/239)). Proven by a storage integration
  suite (unconstrained allows all, a listed parent and root both succeed, an out-of-order
  placement is refused on create and on move with the type names in the error, a cycle move is
  refused distinctly, and an existing noncompliant placement is untouched by an unrelated update
  until something tries to move it), a boot-seed idempotency assertion, and API/web tests
  (including the reparent picker's candidate filtering and self-exclusion). Deferred:
  `allowed_parent_types` for `system_type`/`component_type`, the same read-only-in-edit Parent gap
  on Systems and Components, and a drag-to-reparent affordance (this slice's picker is a select,
  not a drag surface).
- **Collection slice (checkpoint 5g): the derived interface/task model.** The reachability primitives were
  reframed to their honest shape ([ADR-0040](/architecture/decisions/#adr-0040-the-task-is-derived-read-only-plumbing-projected-from-its-interface)):
  the **interface** is the only authored primitive and the **task** is **derived**. An interface is now
  **named by its protocol** (the name derives from its `interface_type`, unique per component), so the create
  surface takes a type, not a free-text name. Creating an interface **derives its one poll task**, so the task
  surface is **read-only**: the `POST` / `PATCH` / `DELETE /tasks` routes and the `task:create` / `task:update`
  grants are gone, leaving `GET /tasks` and `GET /tasks/{id}`. `task.node_name` is dropped and **projected** from
  `interface.node_name` (the worklist and the telemetry owner-confinement now join the interface for placement),
  and a **node purge cascades** its interfaces and their tasks (`interface.node_name` and `task.interface_id` are
  `ON DELETE CASCADE`). The console follows the entity model, folding the collection children onto their parents:
  the standalone Tasks page is removed and a node's derived tasks read as a **panel on the node detail**, and the
  standalone Interfaces tab is removed and a component's interfaces (with their reachability, add, and edit/delete)
  read as a **panel on the component detail** (an interface belongs to its component). The interface create form
  drops the name input (name derives from the protocol), the node detail moves onto the shared read-edit-save blade,
  and the collection pages route through the shared Button primitive.
  Storage, the API, the CLI, and the typed client regenerate from the reduced surface, so `make gen` is clean.
  Proven by the reduced storage, API, and web suites over the derived model (an interface derives its task, the
  task surface is read-only, placement projects, a node purge cascades). This keeps the [collection](/architecture/collection/)
  and [nodes](/architecture/nodes/) pages `Partial`: the driver / collect layer (the normalized menu, SNMP, the
  `$sec:` / `$var:` interpolation consumer, templates) is still Design.
- **Catalog: the `component_make` manufacturer registry.** The first landed slice of a larger
  make/model catalog: a flat, seed-and-custom registry (`id`, `official`, `display_name`, `icon`,
  `support_phone`, `website`) naming who makes a component, on the same official-row-read-only
  pattern as the `*_type` registries. Full CRUD (`POST/GET/PATCH/DELETE/list /api/v1/component-makes`)
  gated by a new, capability-only, unscoped **`make`** resource (`make:read` sits in the viewer
  `*:read` floor, `make:create,update,delete` at the admin tier, mirroring `type:*`); eight
  official makes (Crestron, Biamp, QSC, Shure, Cisco, Extron, Sony, Samsung) are upserted
  idempotently at boot. The `website` field is validated to an `http`/`https` URL on create and
  update, both client and server side (a non-browser caller is refused with a 422), closing a
  stored-XSS path a `javascript:`/`data:` value would otherwise open when the console rendered it
  as a live link; a value that still fails the check renders as plain text, never a dead or unsafe
  anchor. The console ships a **[Makes](/guides/admin/vendors/)** catalog page (`FlatList` + blade,
  an official row read-only, same shape as [Types](/guides/admin/types/)) and the generated CLI
  (`omniglass component-make list/get/create/update/delete`). Proven by a storage round-trip
  (official read-only, duplicate id, alphabetical list), a seed idempotency test, a real-binary API
  test (the CRUD lifecycle, the website-scheme 422, the capability gate), and web unit and
  component tests covering the safe-URL render guard. Deliberate thin cuts: no in-use delete guard
  (nothing references a make yet, so a custom row deletes unconditionally; the referential 409
  lands with `component_model`), no `component_type` genus tree, no `component_model`, and no
  picker wiring a component to a make. Deferred: the rest of the make/model catalog
  ([epic #254](https://github.com/hyperscaleav/omniglass/issues/254),
  [#255](https://github.com/hyperscaleav/omniglass/issues/255)).
- **Field: an operator-defined typed attribute on a type (slice 0).** The first cut of the
  [field](/architecture/variables/#property-one-typed-name-a-product-contract-a-stored-value) primitive: an operator declares a typed
  field on a `component_type` (a `field_definition`: a `name`, a `data_type` of `string`/`int`/`float`/`bool`/`json`, and an
  optional type-level default validated against the `data_type`, unique per `(component_type, name)`), a component sets a
  **literal** for a field defined on its type (a `field_value`), and the component's **effective** value resolves to the set
  literal or the type default (an `is_set` flag marks the override). The whole vertical shipped: storage (transactional,
  audited), the API (the definition catalog flat and `field:<action>`-gated, the value routes ABAC-scoped to the owning
  component), the generated CLI and typed client, and the UI (define on the component-type blade under
  [Types](/guides/admin/types/), plus an **Effective fields** panel on the component detail that sets a literal and shows
  override-versus-default). Owner is the **component only**: macro interpolation (`$var:`/`$sec:`/`$datapoint:` in a value),
  the cross-type cascade (`product → location → system → component`, deepest-wins), the `sources` model, typed `file` fields,
  and definitions on the non-`component_type` owners are deferred to later slices; the UI cannot yet clear a set value back to
  the type default (the clear route exists on the API and CLI but is not wired into the panel). The [config, secrets, and
  variables](/architecture/variables/) page moves to `Partial` for the field member. Proven by a storage integration test
  (definition CRUD and the default type-match guard on create and update, the effective set-or-default read and the
  not-applicable guard, and value update/delete reverting the component to the default), an HTTP e2e (the effective read, the
  override set path, and the viewer-reads / operator-sets authz split at the `field:create` gate), and a scope-resolver unit
  test for the field arc kinds (a component-scoped operator resolves a subtree scope, a viewer resolves empty).
- **Platform: the settings engine (install-wide level).** The [settings](/architecture/settings/) subsystem ships its first
  slice: a pure `settings` package that resolves an effective configuration document by deep-merging ordered layers (embedded
  declared defaults, an optional operator `file`, and the install-wide DB override) most-specific-wins in JSON map-space, tracking
  per-key **provenance** and enforcing a top-down **lock** (broader level wins). The single **`setting_override`** table holds
  only the override level, since the base layers are recomputed in memory each boot and restore is therefore a delete
  ([ADR-0033](/architecture/decisions/#adr-0033-settings-persist-only-the-override-level-base-layers-are-recomputed-in-memory)),
  and its Gateway methods are **unscoped**, gated by the `settings:<action>` permission alone
  ([ADR-0034](/architecture/decisions/#adr-0034-the-settings-gateway-is-unscoped-only-the-permission-gates-it)), reusing the
  [cascade](/architecture/cascade/) primitive on the principal axis
  ([ADR-0035](/architecture/decisions/#adr-0035-settings-resolve-as-a-cascade-over-principals-with-a-broader-wins-lock)). The
  API exposes an admin **`GET /settings`** with provenance, a client-safe authn-only **`GET /settings/me`**, and **`PATCH`** /
  **`DELETE`** / **`POST /settings:restoreDefaults`** writes under `settings:read` / `settings:update`; the generated CLI and
  typed client follow. Two `profile`-domain namespaces seed (**`ui`** and **`keybindings`**), and **`ui.theme` is wired end to
  end**: the console reads its theme from `/settings/me`, so an admin setting the org theme default re-themes the SPA on next
  load. The Admin **Settings** page (namespace sections, provenance badges, lock chips, restore-to-default) replaces the
  `/settings` nav stub. Proven by the pure engine unit suite (deep merge, RFC 7386 merge-patch, cascade resolution, lock
  enforcement, provenance), a testcontainer override round-trip (upsert, audit-in-transaction, delete-restores), an HTTP e2e
  (admin read with provenance, patch-then-read reflects the override, `/settings/me` readable by a non-admin, the admin read
  forbidden to a viewer), and web tests (the page renders a provenance badge; the theme mapper). Deferred to the fast-follow:
  the **group** and **user** override rungs and the Profile preferences tab, the `settings:lock` split for group-admins,
  `platform`-domain namespaces (`retention`, `integrations`), a GitOps read-only mode, and live file reload (SIGHUP).
- **Platform: typed settings (slice 1).** A setting is now declared once as a field on a canonical **`Settings`**
  Go struct: reflection over its `default`, `enum`, `pattern`, and `settings:"<domain>,<visibility>"` tags builds
  the code-defaults layer and the namespace registry (the embedded `defaults.yaml` and the hand-kept
  `Namespaces()` list are retired), so adding a setting is one tagged field with no second place to drift
  ([ADR-0041](/architecture/decisions/#adr-0041-settings-are-a-reflected-typed-struct-with-generated-client-and-server-validation)).
  The cascade still merges partial maps, but the effective read unmarshals into the typed struct: the API `values`
  is now the typed **`Settings`** (the generated client gets `values.ui.theme` as a union), and Go code reads a
  setting off `settingsSvc.EffectiveTyped(ctx)`. Writes validate against the reflected schema (Huma
  `SchemaFromType` plus `Validate`): an unknown namespace is a **404**, an unknown key or a value failing its
  `enum`/`pattern`/type is a **422**. A `make gen` step slices those field constraints out of the OpenAPI into
  **`web/src/api/settings.schema.gen.ts`**, and the settings form validates each field **inline** against it
  (an enum renders as a select of the generated options, a bad value shows an inline message and blocks Save),
  with the server 422 as the backstop, so the console and the server enforce the same rules from one source.
  `default_landing` gains a `^/` pattern to make the inline validation demonstrable. Proven by the reflection
  unit suite (defaults coercion, registry from tags), the validator suite (unknown-namespace, unknown-key, enum
  and pattern violations, null-delete skipped), HTTP e2e (a bad `PATCH` is 422 and leaves no override, an unknown
  namespace is 404), and web tests (the inline validator plus the form rendering the generated enum). Deferred:
  the declarative operator-file machinery (a generated schema for the operator file, file-layer validation, and
  letting the file layer outrank the database at the platform level).
- **Field: an optional `display_name` on a definition.** A `field_definition` gains a nullable
  **`display_name`**, a human label threaded schema through UI: the console shows the label wherever it is
  set (the Fields editor on the component-type blade and the **Effective fields** panel), while the raw
  `name` stays the unique key and the interpolation handle, so an unset label falls back to the key. The
  column is additive and idempotent, presentation only. Proven by the storage round-trip (the definition
  create and update carry the label, an unset label stores NULL), the HTTP e2e (the create body accepts
  `display_name` and it round-trips through the catalog list), and the dev seed (the seeded display fields
  carry labels without changing row counts). The [config, secrets, and variables](/architecture/variables/)
  page keeps its `Partial` field badge.
- **Field: batched edit, upsert, and inherited-versus-set clarity.** The Fields panel's edit flow is
  reworked. Setting a value is now an **idempotent upsert** (`SetFieldValue`): the first set creates, a
  later set patches in place (no more `409 field already exists` on a second set), and a set to the
  unchanged value is a no-op, so the `set-field-value` route returns `200` and stays `field:create`-gated.
  In the UI the per-field save is gone: a field's edit **stages a draft** and the blade's **Save changes**
  flushes every touched field (an upsert, or a delete for a cleared override) alongside the component core,
  through a new `onSave` contributor on the blade edit slot. Edit mode now tells **inherited** from
  **set**: an inherited field is an empty input with a greyed `unset` placeholder, a set field shows its
  value with a **clear (×)** that stages a revert to the type default (replacing the revert arrow). Proven
  by a storage integration test (a second set patches in place, a same-value set writes no audit row), the
  updated HTTP e2e (the second POST upserts to `200` keeping its `value_id`), and a blade-slot unit test
  (contributors flush before the primary save, and a throwing contributor aborts and keeps the blade in
  edit). The [config, secrets, and variables](/architecture/variables/) page keeps its `Partial` field
  badge.
- **Config, secrets, and variables: the per-component effective-secrets and effective-variables panels retire.**
  The standalone **Effective secrets** and **Effective variables** panels on the component detail, and their
  `GET /components/{name}/effective-secrets` / `GET /components/{name}/effective-variables` routes (with the
  generated `omniglass effective-secret list` / `effective-variable list` commands and the matching typed-client
  methods), are **removed** ([#281](https://github.com/hyperscaleav/omniglass/issues/281), under the
  [field](/architecture/variables/#property-one-typed-name-a-product-contract-a-stored-value) epic
  [#266](https://github.com/hyperscaleav/omniglass/issues/266)). The panels listed **every** cascade-resolving cell
  that reached a component, mostly inherited noise; the **field** primitive is the schema-over-cells consumer, so a
  component's values are now its **fields** (override versus type-default, the **Effective fields** panel), and a
  secret or variable reaches a component by being **sourced into a field** (the deferred field `sources` model) or
  **bound to a collection interface input**, not through a per-component cascade-browse panel. **Kept:** the storage
  cascade **resolvers** (`ResolveSecrets` / `ResolveVariables`) as the internal primitive the future `$sec:` /
  `$var:` interpolation consumer will call, and the **Secrets** and **Variables** directories (browse, create,
  edit, reveal) with all their routes and CLI. The [config, secrets, and variables](/architecture/variables/) page
  drops the retired-panel claims and the [decision log](/architecture/decisions/) records the call. No schema
  change: the value cells, the resolvers, and the directories are all unchanged.
- **Node identity and edit (N1).** A node reaches parity with the component/system/location
  primitives: it gains a nullable `display_name` and an optional `location` (a descriptive
  placement referencing `location(name)`, `ON DELETE SET NULL`, not a scope; a node stays
  estate-wide), added by an additive migration. `name` stays the immutable key and estate
  address. `UpdateNode` and `PATCH /nodes/{name}` (gated a new `node:update`, covered by the
  owner `node:*`) patch display_name, description, and location, with an unknown location a 422
  through the FK. The console blade becomes read-edit-save: the title is the display_name
  (falling back to the name), **Edit** is the primary action and Enroll / Re-enroll moves to the
  secondary kebab, the list row labels by display_name with the key and location as its subtitle,
  and the create Drawer carries the two identity fields (its enroll flow is unchanged). Proven by
  the storage round-trip (each patched field, name immutable, location set / clear, the
  `ON DELETE SET NULL`), the real-binary API test (the PATCH round-trip, the `node:update` 403,
  the unknown-location 422, create mints no token in the body), and the web suite (the Edit
  action gated on `node:update`, the editable fields, Re-enroll from the kebab, the labelled
  rows). Deferred to their own issues: the token-after-create flow (#287), node tags (#285),
  decommission (#286), and inline create for flat lists (a platform list-primitive concern). The
  [nodes](/architecture/nodes/) page stays `Partial`.
- **Node tags (N2).** A node becomes a taggable owner kind, alongside the install-wide tier / component / system /
  location. The `tag_binding` owner arc gains a `node_id` leg (an additive migration re-adding the two
  CHECK constraints; `ON DELETE CASCADE` so a node purge drops its bindings), and the governed
  `applies_to` set can now include `node`. A node is estate-wide, not a scope tree, so tagging it needs
  an all scope (`node:update`, reusing the identity gate) and its effective tags are the install-wide layer
  plus its own direct bindings, no inheritance. The generic per-entity tag routes grow the
  `/nodes/{name}:listTags` / `:setTag` / `:removeTag` methods, and the node body carries `effective_tags`.
  The console node blade gains a **Tags** panel (the shared `TagAdder`: read pills, add / remove in edit),
  and the node list a **Tags** column plus a per-key filter facet, the same shape as the component list.
  Proven by a storage integration test (the applies_to gate, the all-scope requirement, effective = install-wide
  + direct, unbind), a real-binary API test (bind / list / effective / unbind and the `node:update` 403),
  and the web suite. Follows [node identity](/architecture/nodes/) (N1); the [tags](/architecture/tags/)
  page stays `Partial`. Deferred: setting tags **during** node create (an inline-create platform concern).
- **Node decommission (N3).** The node blade gets its destructive action: `DeleteNode` +
  `DELETE /nodes/{name}` (gated a new `node:delete`, covered by the owner `node:*`). It is a
  hard delete of the node's `kind='node'` principal, which cascades the node detail and, through
  it, its interfaces and their derived tasks, its node-owned tags and self-telemetry, and its
  enrollment credential, every referencing FK is already `ON DELETE CASCADE`, so no migration. A
  node is estate-wide, so it needs an all scope; the delete is audited, and the component
  telemetry the node collected (owner arc = component, `node_id` null) survives untouched. The
  console blade adds a **Delete** action (confirm, then close and refresh), completing its
  cred-action set (Edit primary, Enroll / Re-enroll kebab, Delete destructive). Proven by a
  storage test (the cascade of interfaces / tasks / tags, the component datapoint surviving, the
  all-scope gate) and a real-binary API test (the DELETE and the `node:delete` 403). Closes the
  node-lifecycle arc (N1 identity, N2 tags, N3 delete). The [nodes](/architecture/nodes/) page
  stays `Partial`.
- **Field: the override model.** Field rendering converges on one generic primitive, **`FieldControl`** (the
  field-facing sibling of the `KVRow` / `KVStacked` key:value pair), consumed everywhere a
  resolved-value-with-override appears. It renders in two modes. **Read** is a slim one-line row (label left,
  value right) where an **override** reads with an accent **dot on its key** and its value in the **accent
  colour**, while inherited stays muted. **Edit** is a stacked cell whose key row carries a right-aligned
  **Override** switch: switch **off** inherits the resolved value (the type default this slice) with no
  editable input; switch **on** reveals a type-aware input seeded from that value, and **revert is the switch
  off** (no separate clear). This fixes the **bool** case the old renderer got wrong: inherited, a bool shows
  the resolved word (`true` / `false`) muted rather than a toggle you appear to have set, and override on
  gives a real editable toggle. A new **`field_definition.required`** boolean (additive migration, default
  false, carried through the effective read) marks a required field with a red **`*`**, forces its override
  on, and gates the blade **Save**: the red input box and a "This value is required" label appear only after
  a submit attempt leaves the field empty, and Save is blocked while any required field is unfilled.
  **`EffectiveFields`** is the first consumer, now rendering every field through `FieldControl` and still
  batching each touched field onto the blade Save. The **`$` source picker** (variable / secret / file) and
  the symbol-plus-name display of a sourced value are **drawn in the control but wired in a later slice**;
  this slice sets literals only. Proven by `FieldControl` unit tests (read dot-and-colour and
  inherited-muted, edit switch-off-value versus switch-on-input and revert, the bool word-versus-toggle
  split, and the required marker plus the submit-only red box and Save gate), the `EffectiveFields`
  blade-batch test, and a storage/HTTP round-trip carrying `required`. The [config, secrets, and
  variables](/architecture/variables/) page keeps its `Partial` field badge.
- **The property catalog.** The `datapoint_type` catalog is generalized into the primitive-agnostic
  **`property`** catalog (the physical table; the concept, the `/properties` API, the Go `Property`, and the
  console all read `property`, while a property's identifier stays a `key`): the typed set of signals a
  datapoint **observes** and a field **declares**. The
  `(scope, name)` ladder collapses to a `name` primary key plus an **`official`** boolean (seed-owned
  properties read-only); `value_type` becomes **`data_type`** over `{string, int, float, bool, json}` (`text`
  backfills to `string`, `bool` added); **`kind`** is nullable (a declared-only attribute property has none);
  and **`validation`** is a **JSON Schema** enforced by Huma's own validator with **no new dependency**.
  Value and source tables keep keying by the **name string** (no FK), so the rename is behavior-preserving:
  the ingest registry, the reachability BFF, and the metric/state sinks are unchanged. A new
  **`internal/key`** primitive holds the one canonical name-format rule (lowercase, dot-hierarchied) and
  the typed value validator; tag's key rule folds onto it. The Storage Gateway gains custom-property CRUD
  (gated `property:create` / `:update` / `:delete`, official properties read-only, audited in the same transaction),
  exposed at **`/properties`** with a generated CLI and client, and a new **Catalog > Properties** console page (list,
  blade, create form). An official seed ships the reachability properties plus a starter attribute set
  (`serial_number`, `mac_address`, `firmware_version`, `model_number`). Proven by `internal/key` unit
  tests, the `property` CRUD integration test, the `/properties` HTTP e2e (the `property:create` 403 and official
  read-only), and the console suite. The type-schema (`field_definition.key`, PR-B) and reconciliation are
  deferred ([ADR-0043](/architecture/decisions/#adr-0043-the-property-catalog),
  [#297](https://github.com/hyperscaleav/omniglass/issues/297)). The
  [config, secrets, and variables](/architecture/variables/) page stays `Partial`.
- **The component classification catalogs.** The `component_make` registry is generalized into a **`vendor`**
  catalog with a **`kind`** (`manufacturer` / `integrator` / `developer`), and two new leaf catalogs join it as
  the rest of the component-classification reference data: a **`driver`** (id, display_name, version) and a
  **`capability`** (id, display_name). Each of the three reuses the `official`-boolean chassis the type and
  property registries prove: seed-owned official rows are read-only (update / delete refused 422), a custom row
  is full CRUD gated by the resource's `<resource>:create` / `:update` / `:delete` permission (admin gains
  `<resource>:*`, the `*:read` floor gives everyone read) and audited in the same transaction, with official
  rows seeded at boot. All three ship end to end: the Storage Gateway CRUD, the Huma surface (`/vendors`,
  `/drivers`, `/capabilities`) regenerated into the OpenAPI document, the cobra CLI, and the typed SPA client,
  plus a gated **Catalog** console page each (`/vendors`, `/drivers`, `/capabilities`) with a list, detail
  blade, and create form, official rows read-only in the UI. Proven by the per-catalog CRUD integration tests
  (official read-only, the scoped permission 403), the HTTP e2e, and the console suites. This is **PR2** of the
  estate-model shift toward property / event / command + vendor / product / driver / capability / standard /
  role / health; `product`, `product_capability`, and `component.product` are the next slice
  ([ADR-0044](/architecture/decisions/#adr-0044-the-component-classification-catalogs)). The
  [core entities](/architecture/core-entities/) page stays `Partial`.
- **The product catalog.** **`product`** lands as the concrete **SKU** that ties the
  [ADR-0044](/architecture/decisions/#adr-0044-the-component-classification-catalogs) leaf catalogs together:
  a **`kind`** (`device` / `app` / `service` / `vm`), an optional `vendor_id` (who makes it) and `driver_id`
  (what talks to it), an optional `parent_product_id` (a variant points at its base product), the `official`
  boolean, and the capabilities it provides through the **`product_capability`** join (a video bar provides
  microphone, speaker, camera, codec; a replace-the-whole-set update). It reuses the `official`-boolean
  chassis: seed-owned official rows read-only (update / delete 422), custom rows full CRUD gated by
  `product:create` / `:update` / `:delete` (the `*:read` floor gives everyone read) and audited in the same
  transaction, official rows seeded at boot. The slice also adds **`component.product_id`**
  (`on delete restrict`), the pointer from a component to the product it **is**, making the product the source
  of a component's shape and retiring the `component_type`-as-shape notion; a product still referenced by a
  component cannot be deleted (409). It ships end to end: the Storage Gateway CRUD, the Huma surface
  (`/products`) regenerated into the OpenAPI document, the cobra CLI (`omniglass product
  list/get/create/update/delete`), the typed SPA client, and a gated **Catalog** console page (`/products`)
  with a list, detail blade, and create form (vendor, driver, parent, and capability pickers), official rows
  read-only in the UI. Proven by the product CRUD integration test (official read-only, the `product:create`
  403, an unknown vendor / driver / capability reference 422, and the in-use 409 when a component points at
  it), the `/products` HTTP e2e, and the console suite. This is **PR3** of the estate-model shift toward
  property / event / command + vendor / product / driver / capability / standard / role / health
  ([ADR-0045](/architecture/decisions/#adr-0045-the-product-catalog)). The
  [core entities](/architecture/core-entities/) page stays `Partial`.
- **The `event` log-kind sink.** The collection pipeline gains its **third sink**. A new **`event`** table is
  the **log-kind sink** (a past occurrence) beside `metric_datapoint` / `state_datapoint` (a sampled present
  value), carrying the **same datapoint owner exclusive-arc** (`owner_kind` plus `component_id` / `system_id` /
  `location_id` / `node_id`, one-set CHECK) and the **same provenance** (`observed` / `calculated` / `intended` /
  `declared`, default `observed`), plus a **`message`** (text) and structured **`attributes`** (jsonb). A
  **log**-kind datapoint that the ingest consumer used to **drop** (it had no sink) now routes to `event`:
  `deriveDatapoints` returns metrics, states, **and** events, and the consumer calls **`InsertEvents`**, so a log
  rides `string_value` (its message) or `json_value` (its attributes) under the **same** owner-confinement and
  reject-not-project gates as the other two sinks. A boot-seed property **`log.line`** (kind `log`) is the
  canonical starter. The reserved **`event_id`** columns on `metric_datapoint` and `state_datapoint` are closed
  into real foreign keys to `event(id)` (`on delete set null`), so an **intended**-provenance datapoint
  references the `event` that produced it. Storage adds `InsertEvents` (batch, in-tx) and `ListComponentEvents`
  (newest first); the read route **`GET /components/{name}/events`** (`list-component-events`, gated
  `component:read`, non-disclosing 404 out of scope) returns the last 24 hours capped at 200, regenerated into
  the OpenAPI document, the cobra CLI, and the typed SPA client, and the component detail page gains an **Events**
  panel over it. Proven by the consumer unit tests (a log routes to the event slice, a metric/state still land in
  their sinks), the `InsertEvents` / `ListComponentEvents` storage integration test, and the
  `/components/{name}/events` HTTP e2e (the newest-first window and the out-of-scope 404). With metric, state,
  **and** log all flowing, this is the **P1 follow-up** of the estate-model roadmap
  ([ADR-0046](/architecture/decisions/#adr-0046-the-event-log-kind-sink)); the
  [datapoints](/architecture/properties/) and [data collection](/architecture/collection/) pages stay `Partial`
  (the `log_datapoint` table, the `event_type` registry, and log-to-event promotion are still `Design`).
- **The fields fold: the product contract and the property value store.** The standalone **fields** feature
  **retires**: a field was never a primitive, it was a **property with `declared` provenance**. Two tables take
  its place. **`product_property`** is the product's declared-property **contract** (`product_id`,
  `property_name`, an optional `default_value`, a `required` flag, unique per pair), replacing
  `field_definition` and its per-`component_type` catalog; `data_type` and `validation` are **not** duplicated,
  they stay on the [property catalog](/guides/admin/properties/). **`property_value`** is the value store,
  carrying the **same owner exclusive-arc** as `metric_datapoint` and `event` (`owner_kind` plus
  `component_id` / `system_id` / `location_id` / `node_id`, one-set CHECK) plus an `instance` discriminator, a
  **`provenance`** (`observed` / `calculated` / `intended` / `declared`, default `declared`), and a jsonb
  `value`; its series key is `unique nulls not distinct`, since the arc leaves three owner columns NULL and the
  default NULLS DISTINCT would let duplicates through. The resolver **`EffectiveProperties`** is one SQL UNION:
  the **contract arm** (every `product_property` of the component's product, valued
  `coalesce(the component's declared value, the contract default)`, `from_contract` true) plus the
  **off-contract arm** (declared values the contract does not declare), so a **productless** component still
  resolves, to its off-contract set alone. Six routes ship, regenerated into the OpenAPI document, the cobra
  CLI, and the typed SPA client: `GET /products/{id}/properties` and `PUT` / `DELETE
  /products/{id}/properties/{property}` (gated `product:read` / `:update` / `:delete`, an official product
  read-only 422), and `GET /components/{name}/properties` plus `PUT` / `DELETE
  /components/{name}/properties/{property}` (gated `component:read` / `:update`, ABAC-scoped with a
  non-disclosing 404 out of scope, audited in the same transaction). The CLI reads `omniglass product
  properties|set-property|delete-property` and `omniglass component properties|set-property|clear-property`.
  The console renames the operator word from **Fields** to **Properties**: the component detail gains a
  **Properties** panel (contract rows, a dashed-bordered **off contract** group for the ad-hoc ones, an
  override toggle with an accent dot on an override, a required property blocking Save), and the product detail
  gains a **Declared properties** contract editor (declare, edit, withdraw, read-only for an official product).
  Retired with the feature: `field_value`, `field_definition`, `component.component_type`, and the
  `component_type` table with its `/types/component` routes, its console registry section, and its seed. A
  component's shape now comes from its **product** (optional: a productless component simply has no contract),
  and the category `component_type` used to carry (display, codec) is expressed by the **capabilities** that
  product provides. The seeded products ship a starter contract (`cisco-room-bar` and `samsung-qm55` declare
  `serial_number`, `firmware_version`, and `model_number` with defaults), and `roles.yaml` drops the
  now-unclaimed `field:*` permissions, since `property:*` already covers the tier. Proven by the
  `product_property` and `property_value` storage integration tests (the in-place contract upsert, a nil
  default round-tripping as SQL NULL, both resolver arms, the idempotent re-set on one series row, the
  clear-to-default, and the productless component), the `/products/{id}/properties` and
  `/components/{name}/properties` HTTP e2e (the official-product 422, the unknown-property 422, the
  out-of-scope non-disclosing 404, the withdraw-twice 404, and the clear back to the contract default), and the
  console suites for both new surfaces. This is **PR5** of
  the estate-model shift ([ADR-0047](/architecture/decisions/#adr-0047-the-fields-fold-product_property-and-property_value)).
  No page badge moves: [core entities](/architecture/core-entities/) and
  [config, secrets, and variables](/architecture/variables/) both stay `Partial` (the cross-owner cascade, the
  non-`declared` provenance producers, and the `standard` / `location_type` contracts are still ahead).
- **The `standard` blueprint, the owner-generic resolver, and the template-fork seed model.** `system_type` is
  **promoted to `standard`**: the blueprint a system conforms to, the system-side counterpart of `product`. The
  renamed table gains **`parent_standard_id`** (variants, mirroring `product.parent_product_id`) and a declared
  property contract, and `system.system_type` becomes **`system.standard_id`**, now **optional**, so a **one-off
  system that conforms to no standard** is first-class exactly like a productless component. The seeded rows carry
  over. Since a standard now owns a contract it is a **Catalog entity, not a bare type registry**: it leaves the
  shared `type:*` permission for its own **`standard:read` / `:create` / `:update` / `:delete`** (read on the
  viewer `*:read` floor, the writes admin-tier, exactly like `product:*`) and its routes move from `/types/system`
  to **`/standards`**. Two contract tables join `product_property` on the identical shape:
  **`standard_property`** and **`location_type_property`** (`<classifier>_id`, `property_name`, an optional
  `default_value`, a `required` flag, unique per pair); `data_type` and `validation` are **never** duplicated onto
  a contract, they stay in the [property catalog](/guides/admin/properties/). The resolver then generalizes to
  **`EffectiveProperties(ctx, ownerKind, ownerID, read)`**, resolving **component, system, location, and node**
  off **one** parameterized SQL template driven by an **`ownerContract`** table (instance table, classifier
  column, contract table, contract key, arc column): component reads through `component.product_id`, system
  through `system.standard_id`, location through `location.location_type`, and a node, having no classifier,
  resolves ad-hoc values only. The two-arm shape is unchanged (contract arm
  `coalesce(the instance's value, the contract default)` with `from_contract` true, UNION the ad-hoc arm), so
  three classifier/instance pairs cannot drift apart. `guardOwnerScope` now scope-checks **every** owner arc on a
  value write, not just the component one. The routes ship regenerated into the OpenAPI document, the cobra
  CLI, and the typed SPA client: the `/standards` CRUD; `GET /standards/{id}/properties` plus
  `PUT` / `DELETE .../{property}` (gated `standard:read|update|delete`); `GET /location-types/{id}/properties`
  plus `PUT` / `DELETE .../{property}` (gated `type:*`, the registry CRUD staying at `/types/location`); and the
  value sides `GET /systems/{name}/properties` and `GET /locations/{name}/properties` plus their
  `PUT` / `DELETE .../{property}` (gated `system:*` / `location:*`, ABAC-scoped with a non-disclosing 404 out of
  scope, audited in the same transaction). The CLI reads `omniglass standard properties|set-property|delete-property`,
  `omniglass location-type properties|set-property|delete-property`, and
  `omniglass system|location properties|set-property|clear-property`. The console gains a **Standards** catalog
  page with a **Declared properties** contract editor (the Products pattern), a contract editor on the location
  type blade, and a **Properties** panel on the system and location details (the component pattern: contract
  rows, a dashed-bordered **off contract** group, an override toggle, a required property blocking Save); the
  **Types** page drops its System tab. The **seed model** is the conceptual half of the slice: a standard and a
  location type are created by **forking an in-code template**, a **one-time fork with no inheritance**, so a
  template can be improved in any release because nothing in any tenant points at it. What lands is therefore
  **operator-owned**: shipped standards and the four shipped location types seed **`official: false`** through
  **seed-if-absent** paths (`SeedStandard` / `SeedLocationType`, `ON CONFLICT DO NOTHING`), never the
  authoritative `Upsert*`, whose `ON CONFLICT DO UPDATE` would silently revert an operator's edit on the next
  boot. Forking applies **template -> standard**, never **standard -> system**: a system **conforms** with
  **live** inheritance. The **canonical catalogs are the exception** and keep the authoritative upsert with
  `official: true`, `property` above all, since it is the shared vocabulary a driver maps onto and a release must
  be able to correct it. Proven by the `standard`, `standard_property`, and `location_type_property` storage
  integration tests, the owner-generic resolver tests across all four owner kinds (including the classifier-less
  node and one-off system), the seed regression test that **edits a seeded standard, re-runs the seed, and
  asserts the edit survived**, the `/standards` and four property-route HTTP e2e suites (the official read-only
  422, the unknown-property 422, the out-of-scope non-disclosing 404 on both the read and the write, the
  withdraw-twice 404, and the clear back to the contract default), and the console suites for the new surfaces.
  This is **PR6** of the estate-model shift
  ([ADR-0048](/architecture/decisions/#adr-0048-the-standard-blueprint-and-the-template-fork-seed-model)). No page
  badge moves: [core entities](/architecture/core-entities/) stays `Partial` (the cross-owner cascade, the
  non-`declared` provenance producers, `system_member` composition, and template pinning are still ahead), and
  [API](/architecture/api/) stays `Partial`.
- **System roles, required capabilities, and the assignment guard.** A system now declares **what it needs
  filled**. **`system_role`** is the slot (a table microphone, a main display), declared either on a
  **`standard`**, where every conforming system **inherits it live**, or **directly on one `system`** (ad-hoc,
  which is how a one-off system gets roles at all). Both owners ride the **same exclusive arc `property_value`
  uses** (`owner_kind` plus `standard_id` / `system_id`, a one-set CHECK, and a `unique nulls not distinct` key
  over the arc columns and the role name, since the arc leaves one owner column NULL). A role carries a
  **`quorum`** (how many components should fill it, floored at one) and requires a **conjunctive** set of
  **`role_capability`** rows: a component must provide **every** listed capability. Two more tables complete it:
  **`component_capability`** (`component_id`, `capability_id`, `present`) is the component's **own** capability
  facts layered over its product's (`present=true` adds one the product does not claim, `present=false`
  suppresses one it does), and **`role_assignment`** records who fills the role in this system, with the
  component FK **`on delete restrict`** so a component staffing a role cannot be deleted out from under it. Two
  resolvers ship with them. **`EffectiveCapabilities(component)`** is the product's capabilities UNION the
  component's additions MINUS its suppressions, so a **productless component resolves to just its own
  declarations**; it is the single definition of "what this component can do" for the whole platform.
  **`EffectiveRoles(system)`** merges the inherited arm (`from_standard` true) with the ad-hoc arm, each role
  carrying its required capabilities, its quorum, its assignments, and the served **`assigned`** and
  **`understaffed`** counts, so no surface does the arithmetic itself. The slice's decision is the
  **guard**: **`AssignRole` refuses (422) when the component's resolved capabilities do not cover the role's
  requirement, and the refusal NAMES the missing ones** (`component "panel-1" cannot fill role "table-mic":
  missing microphone, speaker`, sorted so the same gap always reads the same way), a refusal on **modeled**
  grounds in the same class as the location placement constraint, and named for the same reason. Capabilities became a **resolved** set precisely to make that
  strictness survivable: `product` is optional on a component, so a guard over a product-only fact would have
  locked every productless component out of every role. Eight routes ship, regenerated into the OpenAPI
  document, the cobra CLI, and the typed SPA client: `GET /standards/{id}/roles` plus
  `PUT` / `DELETE /standards/{id}/roles/{role}` (gated `standard:read` / `:update` / `:delete`);
  `GET /systems/{name}/roles` (the resolved read) plus `PUT` / `DELETE /systems/{name}/roles/{role}` and
  `PUT` / `DELETE /systems/{name}/roles/{role}/assignments/{component}` (gated `system:read` / `:update`); and
  `GET /components/{name}/capabilities` plus `PUT` / `DELETE /components/{name}/capabilities/{capability}`
  (gated `component:read` / `:update`). Every system and component route resolves its owner **within the
  caller's scope** first, so an out-of-scope target is a non-disclosing 404 on the read and the write alike, and
  every write is audited in the same transaction. The CLI reads `omniglass standard roles|set-role|delete-role`,
  `omniglass system roles|set-role|delete-role|assign-role|unassign-role`, and
  `omniglass component capabilities|set-capability|clear-capability`. The console follows the property pattern
  one tier up: a **Roles** editor on the standard blade (declare a role, set its quorum, pick the capabilities
  it requires), a **Roles** panel on the system detail (each role with its source, its staffing, an
  **understaffed** marker, and assign / unassign), and a **Capabilities** panel on the component detail
  (the resolved set, with the component's own additions and suppressions marked against its product's).
  The shipped **`meeting-room`** standard
  declares **`room-mic`** (microphone + speaker, quorum 2) and **`main-display`** (flat-panel-display, chosen so
  the shipped Samsung QM55 can actually fill it), **seeded if absent** on the operator-owned lane. Proven by the
  `EffectiveCapabilities` storage integration test (the product's set, an addition, a suppression, and the
  productless component resolving to exactly its own declarations), the `EffectiveRoles` and assignment test
  (both arms resolving, quorum 2 reading understaffed 2 then 1 after one assignment, the idempotent re-assign,
  the **named** shortfall on a display that provides neither microphone nor speaker, a productless component
  that declares what the role needs being staffed successfully, the unassign-twice miss, the unknown-role
  not-found, and the one-off system seeing only its own roles), the `/systems/{name}/roles` HTTP e2e (the
  inherited and ad-hoc rows, the 422 that names the gap, the out-of-scope non-disclosing 404) plus the
  seeded-standard e2e, and the seed regression that **retunes a seeded role's quorum, re-runs the seed, and
  asserts the retune survived**. This is **PR7** of the estate-model shift
  ([ADR-0049](/architecture/decisions/#adr-0049-the-system-role-capability-gated-staffing-and-the-resolved-capability-set)).
  No page badge moves: [core entities](/architecture/core-entities/) stays `Partial` (a role's **impact** on
  health, `system_member` composition, template pinning, the cross-owner cascade, and operational mode are still
  ahead) and [API](/architecture/api/) stays `Partial`.
- **Health: the alarm-capability-role chain, and a verdict recorded as a transition.** A system and a location
  now answer "is this working right now?" and "since when?". An **`alarm`** is **component-local** (a
  `severity` of `info` / `warning` / `critical`, a `message`, a `raised_at`, and a **nullable `cleared_at`**,
  so clearing **keeps the row** and the record of what was wrong outlives the fix), and **`alarm_capability`**
  names the capabilities it **degrades**. That naming is the only route out of the component: a component
  **satisfies** a role only when it provides **every** required capability **and none of those is currently
  degraded**; a role with fewer satisfying components than its **quorum** is **impaired**; an impaired role
  contributes the new **`system_role.impact`** (`outage` / `degraded` / `none`, defaulting to `degraded`,
  landing here because this is the slice that reads it); a system takes the **worst** contribution among its
  roles and a location the worst among the systems placed anywhere beneath it. The verdict domain is
  **`healthy` < `degraded` < `outage`**, and the judgement is a **pure package** (`internal/health`) unit-tested
  with **no database**: `Satisfies`, the quorum boundary, worst-wins, and two deliberate safety defaults in
  opposite directions (an unrecognized **impact** reads `degraded`, so a bad value never makes an impaired role
  silently harmless; an unrecognized **recorded value** reads `healthy`, so one stray row cannot paint an estate
  broken). The conceptual heart is **how health is recorded**. It is written **transition-only** onto
  **`state_datapoint`**, which is already exactly that primitive (a row only when the value differs from the
  last one stored, and `StateTransitions` reads the ordered flips the reachability strip draws), on the owner
  arc with `provenance='calculated'` and `source_rule='health-rollup'`; there is **no new history table**. Two
  alternatives were rejected for failing the requirement that the **edges be accurate weeks later**:
  **compute-on-read** keeps no history at all, and **compute-and-write-through-on-read** keeps a history
  **sampled by whoever opens a page**, so the recorded edge is stamped when somebody looked rather than when the
  estate changed. Instead the verdict is **recomputed at the writes that can change it, in the same
  transaction**: alarm raise and clear, assign and unassign, role declare and withdraw, a quorum or impact
  change, a component capability or **product** change, system create (which gives a system's history a defined
  beginning), a **standard** change (which moves every conforming system), and a **relocation** (recomputing
  **both** the location arrived at and the one left, since that one may have just improved). **A read never
  writes**, and it computes the verdict it serves from the **same resolved rows it displays**, a correctness fix
  for a report that could otherwise say `healthy` beside an impaired `outage` role; recorded transitions stay
  the source for history. The slice also forced a **latent bug** into the open: recording an opening verdict at
  system creation gave every system a `state_datapoint` row from birth, and **every rename then failed on the
  owner foreign key**, because those FKs address the owner **by name** and declared no `ON UPDATE`. Migration
  `20260721170000` re-adds all four `state_datapoint` owner FKs with **`on update cascade`**, which is what
  name-as-address always meant; the same gap on `metric_datapoint`, `event`, `property_value`, `alarm`, and the
  role tables is tracked in [#314](https://github.com/hyperscaleav/omniglass/issues/314). Five routes ship,
  regenerated into the OpenAPI document, the cobra CLI, and the typed SPA client:
  `GET` / `POST /components/{name}/alarms` and `DELETE /components/{name}/alarms/{id}` (gated `component:read` /
  `:update`, an unknown capability or bad severity a 422), plus `GET /systems/{name}/health` and
  `GET /locations/{name}/health` (gated `system:read` / `location:read`, scope-injected with a non-disclosing
  404), each returning the verdict, the contributing roles with their degraded capabilities and the causing
  alarms (or the systems beneath, for a location), and the recorded transitions over the last 30 days. The CLI
  reads `omniglass component alarms|raise-alarm|clear-alarm`, `omniglass system health`, and
  `omniglass location health`, and the seed adds a **`health`** state-kind property. The console carries a
  **health badge** on the system and location details and in the systems list, a **Health** panel that names
  the causal chain role by role (alarm -> degraded capability -> role below quorum -> verdict), a **History**
  strip reusing the reachability availability-strip shape, and an **Alarms** panel on the component (raise,
  clear, and a recently-cleared group). Proven by the
  `internal/health` unit suite (the quorum boundary, a degraded assignee dropping a role below it, worst-wins at
  both levels, the impact mapping, and the verdict round-trip) and the storage and HTTP integration suites (the
  transitions written through the whole chain, an irrelevant degraded capability changing nothing, the report
  naming the cause, impact and quorum, the moves on unassign / standard change / product change / relocation,
  the fresh system's report, **health surviving a rename**, the alarm listing and its refusals, and the
  out-of-scope non-disclosing 404). This is **PR8** of the estate-model shift
  ([ADR-0050](/architecture/decisions/#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain)),
  and it **closes epic [#266](https://github.com/hyperscaleav/omniglass/issues/266)**: the slice that consumes
  what the previous seven built. [Health](/architecture/health/) advances from `Design` to **`Partial`** (alarms
  raised by an `event_rule`, system- and location-owned alarms, the `unknown` verdict, the `global` estate top,
  and the SLI / SLO / SLA and KPI tier are still `Design`); [core entities](/architecture/core-entities/) and
  [API](/architecture/api/) stay `Partial`.
- **System membership** (`system_member`): a component's binding to a system is now a row rather than a
  write-once pointer nobody read, and a role attaches to it. Membership is **many-valued**, so a rack DSP
  serving three rooms is a member of all three, which the old single pointer could not say at all, and
  **`is_primary`** keeps one answer for a question asked with no system in hand (a default for context-free
  callers, not a resolution rule; a partial unique index makes a second primary impossible). **Staffing a
  role creates the membership**, so the two can never disagree, while giving up a role leaves it, because a
  member carrying no role (a power conditioner, a spare) is ordinary. A one-time backfill reads **both** of
  the places membership used to be implied, the role table and the old pointer, since each holds components
  the other drops. The API adds `GET /systems/{name}/members`, `GET /components/{name}/memberships`,
  `PUT`/`DELETE /systems/{name}/members/{component}` and
  `POST /systems/{name}/members/{component}:setPrimary`; the CLI adds `omniglass system
  members|add-member|remove-member|set-primary-member` and `omniglass component systems`. Proven by the
  storage integration suite (the shared device in two systems, the first membership taking the default
  without being asked, a later one not stealing it, the default moving rather than duplicating, assignment
  creating the binding, removal refused while a role is still filled, the cascade from both ends, and the
  out-of-scope non-disclosing 404) and by a backfill test that executes the **shipped migration file** rather
  than a copy, covering the shared device, the pointer-only component, and both-at-once, and asserting a
  second run changes nothing. Resolution behaviour is a **deliberate thin cut**: `component.system_id` stays
  and keeps feeding the four cascade resolvers unchanged, so this slice ships and is verified on its own.
  Slice 1 of epic [#324](https://github.com/hyperscaleav/omniglass/issues/324)
  ([ADR-0051](/architecture/decisions/#adr-0051-membership-is-the-attachment-and-a-role-is-what-it-does));
  [core entities](/architecture/core-entities/) and [API](/architecture/api/) stay `Partial`.
- **Membership-scoped resolution** and the end of the component's system pointer: the tag and variable
  cascades seed their system band from `system_member` rather than `component.system_id`, and tag
  resolution takes the system to resolve against (`GET /components/{name}/effective-tags?system=`),
  falling back to the component's **primary** membership when none is given. A shared device therefore
  answers **differently for each system it serves**, which the single pointer could not express at
  all. **Secrets carry no system band**, on ownership grounds: a credential belongs to the device, not
  to the room it serves. `component.system_id` is **dropped**; the component body now reports `system`
  (the primary, by name) and `system_count`, and the console reads both instead of joining uuids
  client-side. Proven by an integration suite written before the change, since a mis-seeded chain is
  valid SQL that silently returns fewer rows: the same component resolving `prod` for one system and
  `lab` for another, the context-free read following the primary and moving when the primary moves, a
  component in no system resolving the other bands without error, a system the component is not a
  member of lending it nothing, and a system-owned secret never reaching a component. Slice 2 of epic
  [#324](https://github.com/hyperscaleav/omniglass/issues/324)
  ([ADR-0052](/architecture/decisions/#adr-0052-the-cascade-resolves-through-membership-and-secrets-carry-no-system-band));
  [cascade](/architecture/cascade/) and [core entities](/architecture/core-entities/) stay `Partial`.
- **The effective-values resolution view**, resolved per membership: the component detail gains an
  **Effective tags** panel showing, per key, the **winning value**, the tier it won from (global,
  location, system, or the component itself) and the owner's name, with the **shadowed candidates**
  struck through beneath it. `GET /components/{name}/effective-tags` had existed for some time with no
  console consumer, so the provenance it returns was being thrown away and the list's flat pills were
  the only view of a resolved value. A **system selector** appears only when the component belongs to
  more than one system, and switching it re-resolves against that membership (`?system=`), so a shared
  device visibly answers `prod` for one room and `lab` for another. A component in a single system
  sees no selector and reads exactly as before. This closes epic
  [#324](https://github.com/hyperscaleav/omniglass/issues/324): the engine became per-membership in
  slice 2, and this is the surface that teaches it. [Cascade](/architecture/cascade/) and
  [core entities](/architecture/core-entities/) stay `Partial`.
- **A name is the address** ([ADR-0053](/architecture/decisions/#adr-0053-a-name-is-the-address-a-uuid-is-identity)):
  the API accepted names on write and returned uuids on read, so a component created with
  `{"parent": "rack"}` read back as `{"parent_id": "0198f..."}` and no response body could be replayed
  as the request that produced it. Every client had to fetch a second collection and join by uuid to
  render one label. Normalized on component, system, and location (`parent`, `location`) and on the tag,
  variable, and secret bodies, which already carried `owner_name` beside the redundant id. Enforced by
  `TestResponsesAddressEntitiesByName`, a guard over the generated OpenAPI that fails on any field
  naming another entity by uuid, and proven by an HTTP round-trip test on all three entities. **Breaking**
  at v0.0.0, deliberately. The schema stragglers (`secret`, `variable`, `tag_binding` owner arcs) keep
  their uuid keys for now and are called out in the ADR.
- **The owner arcs key by name** ([ADR-0055](/architecture/decisions/#adr-0055-the-tag-variable-and-secret-owner-arcs-key-by-name)):
  the nine arc columns on `tag_binding`, `variable`, and `secret` convert from `uuid references <entity>
  (id)` to `text references <entity> (name) on update cascade`, matching every table from the collection
  era onward and completing what ADR-0053 deliberately left. The two conventions no longer meet inside a
  query: the cascade resolvers projected uuid chains purely to match these three tables, and each chain
  now projects the **name** while still recursing on `parent_id`. Proven by `TestOwnerArcsSurviveARename`,
  which binds a tag, a variable, and a secret at a location, renames it, and asserts all three still
  resolve; **mutation-checked**, since removing `on update cascade` makes the rename fail outright rather
  than merely orphan the rows. `tag_binding.node_id` keeps its uuid (a node is addressed by its enrollment
  identity) and `tag_binding.tag_id` is tracked separately in
  [#340](https://github.com/hyperscaleav/omniglass/issues/340).
- **Every foreign key stores a primary key** ([ADR-0056](/architecture/decisions/#adr-0056-every-foreign-key-stores-a-primary-key)):
  all 30 name-keyed foreign keys convert to the target's primary key (a uuid, `principal_id` for a node),
  reversing the direction ADR-0053 set and ADR-0055 completed. `on update cascade` is gone with them: it
  was write amplification across every referencing row to protect a key that did not need to be a name.
  The conversion also fixed a refusal, not just an inefficiency: `interface.component` referenced
  `component (name)` with **no** `on update` clause, so renaming a component that owned any interface
  failed outright with a foreign-key violation. Verified against the restored pre-conversion schema
  rather than argued. In exchange the API accepts **either** form wherever a reference is written (uuid
  tried first) and every response carries both the name and the id. Shipped in five slices grouped by
  subsystem so each file changed once; the last converts the collection tier (`metric_datapoint`'s four
  arcs, `interface`'s component and node arcs, `node.location_name`) and every remaining node reference.
  Proven by `TestCollectionReferencesSurviveARename`, which renames a component and a location under a
  live interface, task, and datapoint and asserts the node's resolved worklist and the component's
  interface list still come back; **mutation-checked** in both directions. Health keeps names internally
  on purpose, since its advisory lock hashes `health/<kind>/<name>` and a mixed currency would silently
  stop serializing.
- **The cascade's least-specific tier is `platform`.** One word, `global`, named two unrelated things: the
  install-wide **binding tier** and the singleton estate **owner** where health and KPIs roll up. The tier is
  renamed **`platform`** on both axes, at exactly the rung it already occupied, and `default` is documented off
  the axis entirely ([ADR-0057](/architecture/decisions/#adr-0057-the-cascades-least-specific-tier-is-platform-and-a-default-is-not-a-tier)).
  An idempotent migration rewrites `owner_kind` on `variable`, `secret`, and `tag_binding` plus `setting_override.scope`,
  with the check constraints and the partial unique indexes keyed on the value; **no precedence changes, no rows are
  added, and every existing deployment resolves byte-identically after upgrade**. The settings engine's `code` level
  becomes **`default`** and its `global` level **`platform`**, the enum rides `make gen` into the OpenAPI, the typed
  client, and the CLI (`tag setPlatform` / `clearPlatform`, `--owner-kind platform`), and the console renders the tier
  and a declared default as what they are ("Declared default" carries no origin badge, because nobody set it). The
  new capability is authorization: a write at the tier now needs **`platform:<action>`** on top of the resource
  permission, published per route as `x-omniglass-platform-permission` and seeded to `admin` only, so an all-scoped
  operator runs every site without being able to move the install-wide value under them. `platform` joins the
  **sensitive-resource set** (with `secret` and `settings`) so a bare `*:update` cannot reach it: install-wide
  authority is never implied by estate reach, in the rbac core and in the console's mirror of it. The console gates
  every tier control on **both** halves, on all three surfaces that write at the tier: the Settings page, and the
  Secrets and Variables pages, where a caller without the tier half is not offered the Platform scope on the create
  form nor Edit / Delete on a tier row, and reads which capability is missing instead of meeting a 403. **Breaking:** a secret
  sealed at the old tier cannot be decrypted after upgrade, since the AAD binds `ownerKind|ownerID|name|field`
  (accepted deliberately, see the ADR). Proven by a testcontainer upgrade test (pre-rename rows resolve identically
  after the migration, the constraints and unique indexes still hold one row per identity), the settings resolver
  unit suite on the renamed levels, and an HTTP end-to-end authz test (an all-scoped role without `platform:*` is
  refused at the tier and permitted below it, and the seeded roles carry the capability exactly where they should).
  The dev seed carries the no-root-location rule into `make dev`: the example estate is a forest of three unparented
  tops with devices under two of them, so the location binding at HQ visibly misses the East campus and only the
  `platform` value reaches both (proven by a pure fixture test and a seeded effective-tags assertion).
  A closing migration puts `setting_override.scope` under its own CHECK. It was the one tier column with no
  constraint behind it, which is how the rename passed a green suite while the Go layer still wrote `global` and
  orphaned every override in silence. The legal set is the levels that are persisted as rows, today `platform`
  alone: `default` is reflected off the `Settings` struct and `file` is read from disk at boot, so neither can ever
  be a row, and the group and user rungs will widen the CHECK when they land.
  The [cascade](/architecture/cascade/) page moves to
  `Partial`: its binding chain ships for tags, secrets, and variables, while the template bands, group placement by
  weight, rule accumulation, and the resolve view stay `Design`.
- **The node API commands are reachable, and a guard keeps the CLI namespace honest**
  ([ADR-0058](/architecture/decisions/#adr-0058-a-run-mode-is-a-verb-under-its-noun-and-no-command-may-be-shadowed)):
  the hand-written edge run mode and the generated `node` group both registered as `node`, so cobra
  resolved `omniglass node list` to the daemon and it failed asking for `--token`. Every generated node
  command was unreachable while the CLI guide documented them as working. The run mode is now
  `omniglass node run`. The durable half is `TestNoCommandNameCollisions`, which walks the assembled
  tree and fails on any duplicate name: written first, it found the known `node` and `type list` cases
  plus **two nobody had reported**, `grant create` and `grant delete`, where the principal-group
  variants shadow the principal ones and granting a role to a principal has no CLI path at all
  ([#357](https://github.com/hyperscaleav/omniglass/issues/357)). Those four are carried as an explicit
  list that may only shrink, since the guard also fails on an entry that stops colliding.
- **The CLI naming rule is derived from the whole route, not the leaf noun**
  ([ADR-0059](/architecture/decisions/#adr-0059-every-collection-segment-is-a-command-level)): every
  collection segment is a level, so a subresource is addressed under its owner (`component property
  list`, `principal grant create`). The old leaf-noun rule produced **24 collisions across 195
  operations** (`property list` seven ways), each silently unreachable and each patched by hand
  afterwards: `nameOverride` had reached 53 entries whose comments all described the same defect. It
  is now 14, all genuinely non-AIP `/auth` routes. The generator's grouping also became a real
  N-level tree, since bucketing by the first word and naming by the last collapsed three-word paths
  back into collisions. 67 of 202 commands renamed, 135 unchanged, zero collisions. `omniglass statu`
  (the depluralizer eating `status`) is gone. Two guards keep it: the collision test, whose
  known-collision list is now empty, and `TestDocsOnlyNameRealCommands`, which fails when a guide
  teaches a command that does not resolve and immediately found one that had never existed in any
  build plus two with no API route ([#359](https://github.com/hyperscaleav/omniglass/issues/359)).
- **The `/types` umbrella is retired**
  ([ADR-0060](/architecture/decisions/#adr-0060-a-resource-is-one-kebab-case-noun-nesting-means-ownership)):
  the location type registry was addressed two ways, `/types/location` for its CRUD and
  `/location-types/{id}/properties` for its contract, so one entity had two command groups and an
  operator had to know both spellings. The registry CRUD moves to `GET/POST /location-types`,
  `PATCH/DELETE /location-types/{id}`, and the secret shape registry to `GET /secret-types`. The rule
  is now stateable in one line: a resource is one kebab-case noun, and nesting means ownership. The
  `type` command group is gone; `location-type list` is the registry and `location-type property list`
  its contract. Addressing only: same handlers, same permission gates, same scope injection, no schema
  change.
- **"Latest" in a calculated series means the highest id**
  ([ADR-0061](/architecture/decisions/#adr-0061-a-calculated-series-is-current-at-its-highest-id-not-its-newest-timestamp)):
  a health row's `ts` is `clock_timestamp()` evaluated before its identity id is assigned, so two
  concurrent inserts can land with the two orderings disagreeing about which row is current. Every
  production reader already ordered by id; the health test helper ordered by `ts`, which is what made
  `make test` intermittently red with a verdict the engine never produced. `LatestState`, which backs the
  ingest transition guard, ordered by `ts` alone and now tie-breaks on id, so a poll cycle stamping
  several rows in one instant can no longer resolve to an arbitrary one. Reproduced deliberately (six
  concurrent copies of the storage package; never once in nine idle full-suite runs) and verified over 24
  runs under the same load.
- **The variable and secret cascades get their routes.** `GET /components/{name}/effective-variables`
  and `/effective-secrets` resolve the same cascade the tag route resolves, returning the winner **and**
  the shadowed candidates with the tier each came from. The gateway resolvers had been written and
  tested since the cascade slice and were **unreachable**: no route, so no CLI command and no console
  read, while the CLI guide taught `omniglass effective-secret list` as though it worked
  ([#359](https://github.com/hyperscaleav/omniglass/issues/359), found by the docs-command guard).
  The two are gated differently on purpose: a variable read rides `variable:read`, a secret read rides
  **`secret:read`**, which the viewer floor deliberately does not carry, and returns **masked** fields.
  The effective read answers which secret applies and where it comes from, never what it contains;
  plaintext stays behind the audited reveal, proven by a test that fails if the seeded plaintext ever
  appears. The CLI commands generated with no generator change (`component effective-variable list`,
  `component effective-secret list`), which is the parent-qualified naming rule paying off. **Thin cut:**
  the console's resolution panel still shows tags only ([#365](https://github.com/hyperscaleav/omniglass/issues/365)).
- **A list row carries both identities.** Every estate entity has a **key** (`name`, the kebab
  identifier the API and CLI address it by) and an optional **display name**. The console showed one or
  the other and never both, so the string an operator needs for `omniglass component get <key>` was
  invisible on every list but Nodes. The row now shows the display name with the key beneath it, muted
  and in the data face, always visible rather than on hover: hover does not exist on touch, is not
  discoverable, and cannot be selected to copy, and copying it is the point. When an entity has no
  display name the label **is** the key, so it renders once, in the data face, marking it as an
  identifier. **Nothing is derived:** sentence-casing `hq-boardroom-dsp` gives "Hq boardroom dsp", and
  this domain is acronyms (DSP, HDMI, NVX, PTZ, UC, AVoIP), so mechanical casing mangles them and makes
  an absent display name look like a typo rather than an absence. The rule now lives in one place
  (`entityLabel`), replacing **nine** copies of `display_name || name` across the node helper, three
  page mappers, two local `label` helpers, and the owner and parent pickers.
- **Create forms lead with the display name and derive the key as you type.** An operator types
  "Conf Room 301" and the key fills in as `conf-room-301`, editable underneath, instead of having to
  invent a kebab identifier and get the character class right. `deriveKey` folds diacritics (so "Café"
  is `cafe`, not `caf`), collapses punctuation runs, trims leading and trailing separators, and
  respects the 100 character ceiling without leaving a trailing `-`; it only ever produces the empty
  string or a key matching the `^[a-z0-9][a-z0-9-]*$` the API enforces, asserted against that pattern
  directly. **The moment the operator edits the key it becomes theirs and stops following**, which is
  the rule that makes the pattern usable rather than infuriating, and the field says which state it is
  in. An existing entity's key is owned from the start, so relabelling can never rename: the API takes
  a rename explicitly and it stays a deliberate act. The coupling is one primitive (`createIdentity`)
  rather than three copies of a signal pair, on Components, Systems, and Locations.
- **The resolution panel reads all three cascades, in one list.** It explained the tag cascade only; the
  variable and secret cascades had routes since the effective-values slice and no console read. All three
  are the same engine, so they share one table with a **kind** column rather than tabs or stacked
  sections: their bands genuinely differ (a variable seeds its system band from the **primary**
  membership, a secret has **no** system band at all), and one list makes that visible instead of hiding
  it behind a control. A secret shows its provenance and the word `masked`, never a value: which
  credential a device resolves and from where is the operationally useful fact, and plaintext stays
  behind the audited reveal. The system selector appears only when a tag is in play, since it is the
  only kind that answers per-system. A caller without `secret:read` simply sees the other two kinds.
  Fixed on the way: `bandLabel` still said **global** for the install-wide tier, months after
  [ADR-0057](/architecture/decisions/#adr-0057-the-cascades-least-specific-tier-is-platform-and-a-default-is-not-a-tier)
  renamed it to `platform`. Its test asserted `from global` against a fixture whose `owner_kind` was
  `global`, a value the API stopped producing at that rename, so the label and its test were stale
  together and the suite stayed green.
- **`product` and `vendor` take uuid primary keys**
  ([ADR-0062](/architecture/decisions/#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle),
  slice 1 of [#262](https://github.com/hyperscaleav/omniglass/issues/262)): their kebab id becomes `name`, a
  unique **renameable** handle, and five inbound foreign keys move to the uuid (`product.vendor_id`,
  `product.parent_product_id`, `product_capability`, `product_property`, and `component.product_id`). The
  registries were the last place a foreign key pointed at a mutable string, so a product id could not be
  corrected after a typo or a rebrand. The API now carries **both** forms on every body and accepts either
  wherever a product or vendor is named. Proven by `TestRegistryHandleRenameKeepsReferences`, which renames
  both handles under a live sub-product, contract, capability set, and component instance, and asserts every
  reference resolves and reads the new handle; **mutation-checked**. The remaining seven registries follow,
  and five of those are a **rename** of `id` to `name` rather than an addition, since the registries did not
  agree with each other on the column name to begin with.
- **`capability` and `standard` take uuid primary keys** (slice 2 of
  [#262](https://github.com/hyperscaleav/omniglass/issues/262), [ADR-0062](/architecture/decisions/#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle)):
  their kebab id becomes `name`, and eight inbound foreign keys move to the uuid, `capability` from
  `product_capability`, `role_capability`, `component_capability`, and `alarm_capability`; `standard` from
  `standard_property`, `system_role`, its own `parent_standard_id`, and `system.standard_id`. Every read
  that returned a capability set (the effective-capabilities resolver, the role requirement lists, an
  alarm's degraded set, the health rollup) now projects the capability's **name**, so the wire surface is
  unchanged while the storage key is a uuid. Two migration facts from slice 1 recurred and are worth the
  note: a renamed table keeps its old primary-key constraint name (here `standard` still answered to
  `system_type_pkey`), so the drop looks the name up from the catalog rather than guessing; and recreating
  a column drops the NOT NULL and the unique constraints it carried, so an unknown capability that resolves
  to null on a not-null arc is mapped back to the ref-not-found sentinel rather than surfacing as a 500.
  The rename test now renames a capability and a standard under a live product, role, and contract and
  asserts each still resolves and reads the new handle.
- **`property` takes a uuid primary key, and telemetry keys become real foreign keys** (slice 3 of
  [#262](https://github.com/hyperscaleav/omniglass/issues/262), [ADR-0062](/architecture/decisions/#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle)):
  the largest registry conversion. `property`'s kebab id becomes `name`, and its **seven** references
  move to the uuid: four contract/value tables (`product_property`, `standard_property`,
  `location_type_property`, `property_value`) that already referenced it, plus the `key` column on
  `metric_datapoint`, `state_datapoint`, and `event`, which had been a loose property name with **no**
  foreign key. Those three become real `property_id` foreign keys, so a rename now follows an observation
  series, not only a contract. Every telemetry key was already a registered property (reject-not-project
  drops an unregistered name at collection), so the constraint only makes the invariant the database's as
  well. Every read still projects the property's **name** as `key` and `property_name`, so the wire
  surface is unchanged. Proven by `TestRegistryHandleRenameKeepsReferences`, extended to rename a property
  under a live contract, a declared value, and a metric datapoint, and assert the datapoint reads the new
  key. The remaining four leaf registries are slice 4.
- **`location_type`, `secret_type`, `driver`, and `interface_type` take uuid primary keys** (slice 4 of
  [#262](https://github.com/hyperscaleav/omniglass/issues/262), [ADR-0062](/architecture/decisions/#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle)):
  the last of the epic, closing the invariant that no foreign key anywhere points at a mutable string.
  `location_type`, `secret_type`, and `driver` are a **rename** of their kebab `id` to `name`;
  `interface_type` already called its slug `name` and only gains the uuid, as `property` did. Five inbound
  foreign keys move to the uuid: `location.location_type` and `location_type_property.location_type_id`,
  `secret.secret_type`, `product.driver_id`, and `interface.type`. Every read still projects the type's
  **name** (a scalar subquery aliased to the wire field), so the wire surface is unchanged while the
  storage key is a uuid, and the product body now carries a `driver` handle beside `driver_id` as it
  carries `vendor` beside `vendor_id`. `allowed_parent_types` stays a text array of type **names**, not a
  foreign key, since it is a self-reference the placement validator resolves by name. Proven by
  `TestRegistryHandleRenameKeepsReferences`, extended to rename all four under a live location, a
  location-type property contract, a secret, a product, and an interface, and assert every reference
  resolves and reads the new handle; **mutation-checked**. With this slice every registry is uuid-keyed;
  retiring the slug-key carve-out in the doctrine and the guard allow-lists is the epic's closing slice.
- **The slug-keyed exception is retired** (slice 5 of
  [#262](https://github.com/hyperscaleav/omniglass/issues/262), [ADR-0062](/architecture/decisions/#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle),
  the epic's close): a docs-and-guard slice with one real fix. The [api-first](/contributing/api-first/) rule
  now reads "every foreign key stores a uuid, no exception"; [ADR-0056](/architecture/decisions/#adr-0056-every-foreign-key-stores-a-primary-key)'s
  slug-keyed carve-out is marked retired. The `TestReferencesCarryBothForms` guard drops its slug-keyed
  allow-list: the registry references (`product_id`, `vendor_id`, `driver_id`, `standard_id`, and the two
  parents) move into the both-forms rule, and emptying the allow-list **surfaced a real gap**, the
  component response carried `product_id` without the product's name, now fixed by adding the `product`
  handle to the component body (the guard's whole purpose, catching a uuid-only reference). The storage
  helper collapses in step: the per-registry `productRefCol` / `vendorRefCol` and the `registryHandles`
  set fold into one `registryRefCol(ref)`, since every registry behaves identically now. **Epic #262 is
  complete**: no foreign key anywhere points at a mutable string.
- **`role` (IAM) takes a uuid primary key**, the last named entity still keyed by its slug. It gains a uuid
  `id` and demotes the slug to a unique, renameable `name`, and its one inbound reference,
  `principal_grant.role_id`, moves to the uuid. The RBAC engine is unchanged: it keys roles by name and
  expands `inherits` by name, so the well-known handles (`owner` / `viewer` / `operator` / `deploy` /
  `admin`) stay stable and the storage layer resolves `role_id` to a name on read and a name (or uuid) to
  the uuid on write. The owner invariant, previously holding the literal slug `'owner'` in its trigger,
  now resolves owner by name (`(select id from role where name = 'owner')`), and the roles view and grant
  bodies carry the role's name beside the uuid. With this every named entity in the schema is uuid-keyed;
  the only non-uuid identities left are deliberate (`node.principal_id`, the polymorphic `scope_id` /
  `resource_id`, content-addressed `blob` / `task`).
- **The type-registry references carry both forms**: the `interface`, `location`, and `secret` bodies
  gained `type_id` / `location_type_id` / `secret_type_id` beside their name (`type` / `location_type` /
  `secret_type`), which were previously name-only, an inconsistency with the both-forms rule the epic
  enforces everywhere else. `TestReferencesCarryBothForms` now guards **both directions**: the forward
  scan already caught a `*_id` with no name; a reverse pass over a curated set of unambiguous registry
  name-fields (`location_type`, `secret_type`) now catches a registry handle with no id, closing the gap
  that let those name-only references slip past (the forward scan has no `*_id` field to trip on). `type`
  stays forward-covered on the primary interface body alone, since it also names an RFC-9457 error type
  and a secret field's data type. The `property_name` contract and value bodies are a follow-up.
- **The interface's registry field is renamed `type` to `interface_type`** (with `interface_type_id`),
  matching `location_type` and `secret_type` so the interface reference reads uniformly and can join the
  guard's reverse set (a bare `type` could not, since it also names an RFC-9457 error type and a secret
  field's data type). The create flag becomes `--interface-type`. The diagnostic reachability row keeps its
  `interface_type` name-only (a display value never fed back to a write), recorded as an explicit
  reverse-check exemption.
- **The declared-property contract and value bodies carry `property_id`** beside `property_name`, the last
  name-only registry references: the product / standard / location_type contract lines, the
  component / system / location value bodies, and the effective-property resolution. `property_name` joins
  the guard's reverse set, so a contract or value body can no longer name a property without its uuid. The
  **event body carries `property_id` beside its `key`** too (`key` is the datapoint-key vocabulary for the
  property name on the telemetry bodies): the event row already stored `property_id` exactly as metric and
  state do, so exposing it is just a projection. The guard accepts `key` as property_id's name pair on the
  telemetry body; `key` stays forward-covered rather than joining the reverse set, since it is also a
  tag-binding field and so ambiguous. Every registry reference in the API now carries both forms.
- **The migration chain is collapsed to a single init.** The 69 per-slice migrations that carried the
  schema from the first commit through the uuid-key epic are squashed into one `db/migrations` file that
  reproduces the current schema exactly. Verified by round trip: a fresh apply of the full chain and a fresh
  apply of the lone init produce a **byte-identical** `pg_dump`. It is pure DDL (no seed rows, which a squash
  drops), reuses the original initial migration's version so an existing database treats it as already
  applied, and carries a real `down` that drops every table. The workflow is unchanged going forward: the
  next schema change is still a new migration on top, never an edit to this one. One migration-history test
  that rolled the database back to a now-absent version was removed; the resolution behaviour it checked is
  covered by the cascade suite. (The stale constraint names the renames left, `component_make_*` on `vendor`
  and so on, are preserved faithfully by the squash; renaming them is a separate cosmetic pass.)
- **The observation tables lose the `_datapoint` genus**: `metric_datapoint` becomes `metric` and
  `state_datapoint` becomes `state`, matching their bare-noun sibling `event` on the principle that the
  noun is the entity and classification takes a `_type` / `_kind` suffix (the `datapoint_type` classifier
  already became `property`). Each table name now equals the `property.kind` value it stores
  (`kind='metric'` to `metric`), and the freed name `datapoint` stays available for the future UNION view
  over the kind-tables. Verified collision-free (not Postgres keywords, no existing table or column named
  `metric`/`state`/`log`). Dependent object names (pkey, sequence, indexes, FK and CHECK constraints) keep
  their `_datapoint` identifiers for now, as the `#262` renames left `vendor` and `standard`; the pending
  migration collapse cleans every stale identifier in one pass. Table-only rename, no data or wire change.
- **The stale schema identifier names are cleaned, and two role tables disambiguated.** The collapse
  preserved every dependent object name verbatim, so the single init carried constraints, indexes, and
  sequences whose prefixes no longer matched their tables: `metric_datapoint_*` and `state_datapoint_*` on
  the renamed `metric` / `state`, `component_make_*` on `vendor`, `system_type_*` on `standard`,
  `datapoint_type_*` on `property`. Each is brought back in line with its table, and the `#262` artifact is
  fixed too: every registry had a `*_uuid_id_not_null` on its uuid `id` column while the text handle kept
  the old `*_id_not_null`, now `*_id_not_null` on `id` and `*_name_not_null` on `name`. Separately,
  `role_capability` and `role_assignment` become `system_role_capability` and `system_role_assignment`:
  both belong to the AV [system role](/architecture/core-entities/) (a slot a component fills in a system),
  not the IAM `role` table, and the bare `role_` prefix invited exactly that confusion. Identifiers only,
  no behaviour change: the init applies clean and the full suite is green.
- **The name foundation of [ADR-0063](/architecture/decisions/#adr-0063-the-telemetry-model-is-typed-registries-over-bare-noun-data-tables) lands: `property_type`, `property`, `property_type_id`.** The
  signal-definition registry (long carried in the docs as `datapoint_type`, and built as `property`)
  becomes **`property_type`**; the latest-value store `property_value` takes the freed bare noun
  **`property`**; and every data table's FK to the registry (`metric`, `state`, `event`,
  `product_property`, `standard_property`, `location_type_property`, and `property` itself) is repointed
  `property_id` to **`property_type_id`**, on the wire as well as in the column. A guarded dbmate
  migration does the table and column renames (the stale dependent object names are left for a cosmetic
  pass, as the collapse did); the storage structs (`PropertyType`, `Property`), the API bodies, the
  generated OpenAPI, typed client, CLI, and JSONSchema, and the console all follow. This closes the
  `datapoint_type`-to-`property` divergence the docs carried: the registry is `property_type`
  everywhere. The rename is **complete, not a thin cut**: the public resource surface moves too (the
  registry is `GET/POST /property-types` under the `property_type:*` permission, while the owner-scoped
  `/{owner}/properties` value routes stay), the both-forms name pair reads `(property_type_id,
  property_type_name)`, the audit resources are `property_type` / `property`, and a second migration
  block renames every dependent constraint and index in line with its table. The fuller ADR-0063 model
  (the provenance-keyed cache, the `event_type` family, the `command` pillar) is still staged.
- **A component's product, location, and parent, and a system's location and parent, become patchable
  ([ADR-0064](/architecture/decisions/#adr-0064-placement-and-classification-are-mutable-after-create)).**
  These fields were accepted on create but had no update path, so a mis-classified or physically moved
  entity could only be deleted and recreated, losing its telemetry history. `PATCH /components/{name}`
  and `PATCH /systems/{name}` now take them on the house three-state (omitted unchanged, empty string
  clears, name sets); a reparent is cycle-guarded and scope-injected like a location move, and a product
  swap keeps hand-set property values while the new contract's defaults follow. No new UI: this opens the
  API and CLI and unblocks the CRUD form primitive's generated edit form. Closes
  [#342](https://github.com/hyperscaleav/omniglass/issues/342).
- **The `property` latest-value cache and the reconciliation read land ([ADR-0063](/architecture/decisions/#adr-0063-the-telemetry-model-is-typed-registries-over-bare-noun-data-tables), first feature slice).**
  `property` becomes the single-lookup latest-value cache for the producer provenances: the telemetry
  ingest sink, after appending the `metric`/`state` sample, derives the newest value per `(owner,
  property_type, instance, provenance)` series through a new `UpsertProperties` gateway, where an
  out-of-order guard keeps a late older sample from displacing a newer latest. The derive is
  **non-gating** (a failed upsert is logged, never fails the ack, and the cache is rebuildable from the
  append-only samples) and idempotent, so a redelivery does not double-write. `LatestValue` returns the
  current value in one lookup instead of an `order by ts` scan, and a **reconciliation** read (`GET
  /components/{name}/reconciliation`) pivots **want** (declared, resolved live from the cascade via
  `EffectiveProperties`, never a cache row), **told** (intended), and **is** (observed), with config-drift
  computed on read; a read-only Reconciliation panel on the component detail teaches the pivot. Only
  `observed` has a live producer today; `intended` (the command pillar) and `calculated` (the calc engine)
  are tested cache slots. One additive migration adds the value's own `ts` to `property`. The
  [datapoints](/architecture/properties/) and [config, secrets, and variables](/architecture/variables/)
  pages stay `Partial` (the cache and reconciliation are built; the intended/calculated producers,
  log-to-event, calc, and fusion remain). Part of
  [#394](https://github.com/hyperscaleav/omniglass/issues/394).
- **The event_type family lands: events get their own registry ([ADR-0063](/architecture/decisions/#adr-0063-the-telemetry-model-is-typed-registries-over-bare-noun-data-tables), second feature slice).**
  Events move out of `property_type` into their own **`event_type`** registry, the occurrence keyspace and
  the twin of `property_type`. A guarded migration creates `event_type`, repoints `event` off
  `property_type` (kind=log) onto `event_type`, adds the **`origin`** column (caught/caused/derived/
  scheduled) plus the `caused_by_event_id` and `correlation_id` causation columns, and drops the `log` kind
  from `property_type` (now `{metric, state}`). Event rows are ephemeral telemetry logs, so the repoint
  clears the occurrence log rather than backfilling a type. The registry ships full CRUD (`/event-types`,
  gated `event_type:*`, seeded official `call.started`, official rows read-only), a
  generated CLI and typed client, and a console **Event Types** catalog page mirroring Properties.
  The **native caught path** is built: a component that publishes an event (an xAPI event, a trap) has its
  registered `event_type` name route it to the `event` sink with `origin=caught`, under the same
  owner-confinement and reject-not-project as a metric or state, and the events panel shows the origin. Raw
  log lines are a **separate ingest lane**, not events ([ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events)):
  the `log_line` table and the derivation rules that turn a log line into an event are a later slice. Only
  the caught path has a producer today; `caused` is the command pillar (#396) and `derived`/`scheduled` need
  the rule engine and the clock (the action layer).
  [events](/architecture/events/) advances `Design` to **`Partial`**; [datapoints](/architecture/properties/)
  notes the `log` kind's graduation. Part of
  [#395](https://github.com/hyperscaleav/omniglass/issues/395).
- **The command pillar lands: the do primitive and computed settlement ([ADR-0063](/architecture/decisions/#adr-0063-the-telemetry-model-is-typed-registries-over-bare-noun-data-tables), third and final slice).**
  Events know, datapoints happen, and now a **command** records what a component was told, the third
  registry (`command_type`) alongside `property_type` and `event_type`. A migration adds `command_type`
  (with a driver-owned `settle_window_seconds` and an optional `target_property_type`) and the `command`
  invocation table over the owner arc. **Issuing** a command is one transaction that composes the whole
  model: it writes the `command`, a **caused** event (`origin=caused`, typed `command.issued`, #395), and,
  for a settleable command, an **intended** value in the property cache (#394) that the observed value
  settles against. **Settlement is computed, never stored:** within the settle window a difference is
  pending, past it a match is settled and a mismatch is failed, the windowed form of the reconciliation
  read's want/told/is. The registry ships full CRUD (`/command-types`, gated `command_type:*`, seeded
  `set_input`/`reboot`, plus a `video.input` state and a `command.issued` event_type), the `POST
  /components/{name}/commands:issue` custom method (gated `command:issue`), a generated CLI and client,
  and a console **Command Types** catalog page mirroring the property and event catalogs. Deferred (the
  actuation half): the outbound push to a driver or node, the protobuf `Command` on the wire, and the
  device acknowledgement. A new [commands](/architecture/commands/) page lands at `Partial`. Part of
  [#396](https://github.com/hyperscaleav/omniglass/issues/396).
- **The datapoint concept retires for property, sample, and current value ([ADR-0065](/architecture/decisions/#adr-0065-property-sample-and-current-value-replace-the-datapoint)).** The one word "datapoint" split the signal from the observation: a **property** is the signal (registry `property_type`), a **sample** is one timestamped observation of it (a `metric` / `state` row, the kind), and the **current value** is the latest sample per series (the `property` cache). A behavior-preserving rename, no tables touched: the proto `Datapoint` message becomes `Sample`, the metric and state datapoint Go types become `*Sample`, `deriveDatapoints` becomes `deriveSamples`, and the value-model page is now [properties](/architecture/properties/) (redirect from the old slug), which carries the current-value cache and want/told/is reconciliation explainer. The ADR also settles that the built `property` cache, a table upserted from the sink rather than the once-sketched metric view, is the architecture-of-record for current values. Part of [#405](https://github.com/hyperscaleav/omniglass/issues/405).
- **The platform ERD is generated from the schema ([data model](/architecture/data-model/)).** A new
  `make gen` step (`cmd/erdgen`) applies the embedded migrations to a throwaway Postgres, introspects
  `information_schema` (tables, primary and foreign keys), and renders a subsystem-clustered D2
  entity-relationship diagram onto the new **Data model** page, so the diagram can never drift from
  the tables. The `internal/erd` render core is pure and golden-tested; the introspection and a drift
  gate (regenerate, compare to the committed D2) run against the real schema. Tables land in
  hand-mapped subsystem containers (identity, estate, catalog, telemetry, collection, config, content,
  audit); an unmapped table renders in a visible `unclustered` box that trips the drift gate. The
  sql_table shapes are themed from the brand tokens so the ERD tracks the light/dark toggle. Part of
  [#406](https://github.com/hyperscaleav/omniglass/issues/406).
- **The raw-log ingest lane is built for the read path ([ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events)).**
  `log_line` is the raw-arrival table: untyped device text owned through the exclusive arc, with
  `severity` and `facility` promoted to indexed columns for retention and routing, and freeform
  `attributes`/`labels`. A log line is not an event, so the write has no registry gate. The read is
  `GET /components/{name}/logs` (gated `component:read`, scope-injected), with a generated CLI and typed
  client, and a console **Logs** panel beside the events panel (severity badges, the structured
  `attributes` and `labels` columns on demand). The dev seed installs the lobby display's device logs (link, CEC, EDID, input, thermal) as log
  lines, their model-correct home. The generated ERD picks up `log_line` in the telemetry cluster. Still
  directional: the ingest producer, the derivation engine that turns log lines into events, and the
  lineage columns. Part of [#410](https://github.com/hyperscaleav/omniglass/issues/410).
- **The event lineage columns land ([ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events)).**
  `event.caused_by_event_id` is renamed to **`source_event_id`** (the source is the cause), and
  **`source_log_line_id`** (an FK to `log_line`, the raw line a rule derived the event from) and
  **`derived_by_rule_id`** (which rule) are added, all nullable, so a natively-caught event carries none.
  This is the schema the derivation engine writes into: a rule that turns a `log_line` into an event will
  stamp the two new columns. No producer fills them yet (the derivation engine is the next slice); the
  generated ERD carries the new shape and the `event -> log_line` edge. Part of
  [#410](https://github.com/hyperscaleav/omniglass/issues/410).
- **The raw-log lane gets its first producer: node self-logs ([ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events)).**
  The telemetry `Event` gains a `LogLine` message and a `repeated logs` field: raw log lines are untyped
  arrival, not registry-resolved samples, so they need their own wire slot. The node now runs an slog handler
  that writes to its console and buffers every record, **installed as the process default so it is a node-wide
  emitter**: any node code ships a self-log with a plain `slog.Info(...)` (carrying a `facility`, and optionally
  a `source`, which the mapping lifts into the `log_line` columns), with no logger threaded down to it, and the
  buffer is bounded (oldest dropped, count reported) so a chatty stretch between ticks cannot grow the edge
  process without bound. The run loop drains it each tick and publishes the
  records as a logs-only Event, which the ingest consumer routes to `log_line` **owner-bound to the node**,
  ahead of the sample owner-confinement and independent of any task (a logs-only Event lands without a
  `task_id`). The node narrates its operational story back to the platform (connected to the bus, worklist
  pulled, a task skipped) instead of swallowing it, and a skipped task becomes a node self-log, not a
  false-down component state. The read is `GET /nodes/{name}/logs` (gated `node:read`, scope-injected), with
  a generated CLI (`node log list`) and typed client; the console reuses the `LogsPanel` primitive as a
  **Self-logs** panel on the node blade. That panel's disclosure is now named for the columns it reveals
  (`attributes`, `labels`, or `attributes + labels`, each block captioned once open) rather than the
  overloaded "fields", which collided with a component's property fields and said nothing about which of
  the two a line carries. The dev seed installs the edge node's example self-logs, and one seeded device
  log line is **classified with a label**, the ADR-0066 way an untyped line gets sorted into a class
  without a registry, which nothing in dev exercised before. The
  end-to-end producer loop is closed by a real node publishing a `worklist pulled` self-log that lands on
  its `log_line` lane. Still directional: a device-log source (a syslog listener or a poll) and the
  derivation engine that turns log lines into events. Part of
  [#410](https://github.com/hyperscaleav/omniglass/issues/410).
- **The telemetry wire message is renamed: `Event` becomes `TelemetryBatch`
  ([#424](https://github.com/hyperscaleav/omniglass/issues/424)).** The protobuf message a node ships was
  called `Event`, but an event in this platform is a semantic occurrence (`event_type`-registered, carrying
  an `origin`, per [ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events)),
  while the message is the **envelope** that carries samples and, since the log lane landed, raw log lines.
  Naming the envelope after one of its passengers had put four different meanings on the word "event",
  three of them in a single function signature. So `proto/og/v1/event.proto` becomes `telemetry.proto`,
  the message becomes **`TelemetryBatch`** (head-noun-last, matching the subject it travels on,
  `og.v1.telemetry.<node>`), and the storage write structs align with the `LogLineWrite` convention the log
  lane established: `MetricSampleEvent` and `StateSampleEvent` become **`MetricSampleWrite`** and
  **`StateSampleWrite`** (they are insert structs, not events), and `EventOccurrence` becomes
  **`EventWrite`**. Wire-safe by construction: protobuf encodes field numbers, never message names, so the
  bytes are identical before and after and a mixed-vintage node and server still interoperate. No behavior
  change, no schema change, no API surface touched.
- **Push ingest: the API telemetry lane ([#423](https://github.com/hyperscaleav/omniglass/issues/423)).**
  A second ingest lane beside the node's, so data can arrive without a driver, a node, or a protocol
  client. **`POST /telemetry:push`** takes telemetry in our own schema (a *webhook*, by contrast, is the
  case where we do not control the payload and something must normalize it, which is driver work) and is
  gated by **`telemetry:push`** with the caller's **scope as the fence**: the owner is declared in the
  body and read back through the gateway, so an out-of-scope owner is a non-disclosing 404. That is the
  inverse of the node lane, where the server must derive the owner because a node cannot be trusted to
  assert one.
  The payload is **two arrays, split typed versus untyped**: `samples[]`, where the **registry** decides
  the destination (metric, state, or a caught `event`), and `logs[]`, which are untyped and ungated. The
  caller supplies a name, never a table, so reject-not-project stays the only authority over where a
  value lands. Rejection is **synchronous** (validated at the route against the in-memory registry
  snapshot) while acceptance stays asynchronous, because a 202 that reveals nothing is useless to a human
  who mistyped a name.
  A push **publishes onto the data lane** (`og.v1.api.telemetry`) rather than writing Postgres directly,
  since the rule engine is designed as a stream consumer and anything skipping the stream would be
  invisible to every rule, alarm, and derivation. Trust is decided by **which subject a batch arrived
  on**: the lane sits outside the single-token `og.v1.telemetry.*` wildcard (a node named for any literal
  reserved there would otherwise be granted it), only the server's credential can publish to it, and a
  node-lane batch that asserts an owner is **dropped, not ignored**. Both lanes land through one
  extracted **`land()`**, so a new lane cannot drift from the node lane's semantics.
  Three proto fields earn their place: `owner`, `source` (the node lane fills provenance from
  `interface_type`, which a push has none of), and `Sample.instance` (a first-class dimension the wire
  could never express, since the node lane smuggles it via the interface name). Both fall back to the
  interface-derived value, so the node lane is unchanged. Still directional: owner kinds beyond
  `component` ([#422](https://github.com/hyperscaleav/omniglass/issues/422)) and webhook normalization.
- **Templates are redefined, not deferred
  ([ADR-0071](/architecture/decisions/#adr-0071-a-template-is-a-clonable-example-not-a-versioned-shape-an-instance-pins)).**
  The decision log and the code had drifted into disagreement and neither said so. ADR-0045 deferred "a product's own
  template or field-schema binding" and ADR-0049 stated "Templates and their frozen BOM stay `Design`", so the log
  asserted the pinned model was merely *deferred*, while the shipped schema pointed a component at `product_id` and a
  system at `standard_id` and ADR-0047 and ADR-0048 had retired `component_type` and `system_type` outright.
  A template is now an **example configuration an operator clones**: forking is one-time, with no inheritance and no
  back-pointer, so **a template is upgrade-safe precisely because nothing stays connected to it**. The versioned-shape
  model retires with the pin (`component_template`, `system_template`, their `*_version` rows, the `stable` / `beta`
  channels, the frozen BOM, "an instance pins a version"). A component's shape is its **`product`**; a system's is the
  **`standard`** it **conforms** to with live inheritance. Forking applies template to row, conformance applies row to
  instance, and only the second is live. The word survives with its purpose inverted: the pin existed so a template
  could not change under an instance, the fork exists so it can change freely.
  `templates.md` is rewritten to the fork model (its frontmatter description carried the retired nouns into search
  results), the cascade loses two rungs (a template cannot be a resolution tier when nothing points at it), and the
  glossary's two shape rows become supersession entries. Two denylist entries land with it, scoped to the snake_case
  identifiers on purpose: the *word* template is not retired, so prose like "start from a template" stays correct.
  Still `Design`: the template loader, its catalog, and the create-from-template flow
  ([#317](https://github.com/hyperscaleav/omniglass/issues/317)), which now also covers instantiating a whole system.
- **The collect layer is authorized: a driver consumes transports, and a transport is code
  ([ADR-0073](/architecture/decisions/#adr-0073-a-driver-consumes-transports-a-transport-is-code-not-a-row)).**
  [ADR-0039](/architecture/decisions/#adr-0039-an-interface-is-a-device-api-the-interface-type-is-its-transport-not-its-driver)
  recorded the driver-centric split as "the current-best direction, **not a locked gate**" and named its own
  successor: driver-centric versus template-centric had to be re-examined "in a later ADR **before the collect layer
  is built**". That ADR was never written, so every collect slice sat behind the scope gate. This is it, and it
  confirms rather than reverses: `interface_type` is the **transport**, decided to be a **code** registry (one
  package per transport) rather than an operator-editable table, which retires the table, its FK from
  `interface.type`, and the hand-written dispatch switch in `internal/node/probe.go` in a later slice. A **driver**
  declares the transports it can run over and is never one of them. The `interface_type` **is** a driver clause in
  ADR-0067 is **retracted**: a calendar integration is an `https` transport plus a `graph-calendar` driver, which is
  the drift a table invites, since any string fits where a transport belongs. Two denylist entries land with it,
  matching the **claim** rather than the two nouns, so ordinary prose naming both a transport and a driver stays
  correct. Nothing here is built: `driver` still carries identity only (name, version, official), and the catalog,
  the normalized menu, discovery, and the device pack are the [collect layer
  epic](https://github.com/hyperscaleav/omniglass/issues/489). Deliberately left open, each with a home: whether a
  product may bind more than one driver, whether a product is versioned or stays a live classifier
  ([#491](https://github.com/hyperscaleav/omniglass/issues/491)), and where cadence lives.
- **The feature-loop framework: work defined once, approved once, shipped as one rollup PR
  ([ADR-0074](/architecture/decisions/#adr-0074-an-approved-definition-rolls-up-to-one-pr-slices-cascade-on-an-integration-branch)).**
  The [feature loops](/contributing/feature-loops/) contract page defines how a body of work executes through an
  agent loop with exactly two human touchpoints: the architect approves a prose definition at the front and merges
  one rollup PR at the end, while sub-issue slices cascade through per-slice gates (test-first, the docs lints, an
  adversarial review) on an integration branch between them. Four skills operationalize it: `/define-work` (the
  nine-section approvable shape), `/run-feature-loop` (the executor run-book), `/add-api-route` (the
  middle-of-slice procedure, built from a code study so every named symbol resolves: `gated(...)`, the scoped-CRUD
  primitive, `writeAuditRes` in-transaction, the ADR-0062 addressing rule, the gen ripple), and
  `/adversarial-review` (refute-not-confirm, a concrete failure scenario per finding, sweeping a twelve-entry
  anti-pattern catalog seeded from the 2026-07-30 audit's failure classes). Generate-first is codified as a house
  rule with its `/ship-slice` gate, and the harness gains unattended-run plumbing (a permission allowlist and a
  SessionStart environment check pinned to the gen-drift versions). Built as the
  [AI-driven feature loops epic](https://github.com/hyperscaleav/omniglass/issues/488); the pilot loop
  ([#502](https://github.com/hyperscaleav/omniglass/issues/502)) runs one approved drift batch through the whole
  frame before any feature loop launches.
- **The generated-facts rails: schema, seed, and environment claims can no longer drift
  ([#433](https://github.com/hyperscaleav/omniglass/issues/433)).** Three artifacts now render the fact classes the
  2026-07-30 audit found drifting worst, each committed by `make gen`, gated by `gen-drift.yml`, and pinned by its
  own drift test. `schema.json` (erdgen's introspection, extended to nullability, defaults, CHECK constraints, and
  unique indexes) feeds the `SchemaTable` component, and eleven architecture pages plus [tags](/architecture/tags/)
  now render their storage-table mechanics from it, hand-written narrative kept as notes; the sweep verifies no
  remaining hand-written row claims a live table's columns. `seed.json` (seedgen, no database: the twelve embedded
  seed YAMLs, with role rows carrying **effective** permissions through the same rbac path the authorizer uses)
  feeds `SeededSet`, and five admin guides render their shipped-set claims from it, retiring the enumerations that
  undercounted the property catalog. `config.json` (configgen) renders the new **declarative environment
  registry**: every variable the binary reads is declared with its doc and scope, `config.Get` refuses an
  undeclared key, and the container and Helm guides render their env tables from it (the Helm guide previously
  documented none). Two API guards land with it: every gated operation's description must name its
  `x-omniglass-permission` stamp verbatim (one shipped deviation corrected: `principal:purge:admin`), and api.md's
  last hand-written route table is replaced by the generated `/reference/api/`. Built as the
  [#502](https://github.com/hyperscaleav/omniglass/issues/502) pilot of the feature-loop framework; the run's
  lessons landed on [feature loops](/contributing/feature-loops/) in the same PR.
- **Scope and secrets integrity: the schema admits only what the model allows
  ([#431](https://github.com/hyperscaleav/omniglass/issues/431)).** The unifying idea, a value the code refuses must
  not be a value the schema, the API, or the console offers, closed across eight slices. The secret directory now
  **injects scope into its query** through the new arc-scope primitive (`arcScopeCTEs` / `arcScopePredicate`, the
  exclusive-arc counterpart of the scoped-CRUD walk; one query regardless of row count), retiring the audit's
  `unscoped-gateway-list` anti-pattern; the secret **system band** is gone from the CHECK, the enum, the console
  picker, and the docs (finishing ADR-0052); the sample provenance domains lost their impossible `declared` arm;
  `scope_kind='group'` stopped being advertised by the schema and both grant enums (group-as-scope returns with its
  slice); the decoy `platform_setting` table and the eight unrouted seed grants are gone, the ship-ahead
  allowlist now deliberately empty under a reversed policy (a grant lands with its first route); the Helm chart
  defaults session cookies to `Secure`; and `alarm` gained its **condition identity**
  ([ADR-0075](/architecture/decisions/#adr-0075-an-alarms-condition-identity-is-a-raiser-supplied-dedup-key)): a
  `dedup_key` plus the `alarm_open_condition_key` partial unique index make one-open-per-condition a database fact,
  with `RaiseAlarm` a guarded conditional insert returning the existing incident instead of a duplicate. Run as the
  second feature loop on the ADR-0074 machinery.
- **Console address honesty and the audit diff
  ([#432](https://github.com/hyperscaleav/omniglass/issues/432)).** The console now addresses everything the way
  [ADR-0062](/architecture/decisions/) promised: the uuid is identity, the kebab `name` is what an operator reads,
  types, and posts. Nine slices: the location_type and secret_type pickers post the handle the write paths resolve
  (a fresh install could not add a location from the console before); every registry write accepts either form
  through `registryRefCol`; the six catalog pages (Drivers, Vendors, Products, Capabilities, Standards, Types) lead
  with a Name column, filter and address rows by the handle, and keep the uuid as a dim blade fact; Products
  renders vendor/driver handles and its capability picker joins on the names the wire carries, so removal works; a
  successful create now lands the operator on the new row's blade across all twelve catalog pages; the 52 blank
  CLI-reference flag descriptions are filled at their Huma `doc:` tags and a docslint check keeps the class from
  recurring; and the audit read finally answers "and to what?": `old`/`new` ride the wire and the Audit page's
  first detail surface renders a field-level before/after diff (a pure `diffFields` projection), with redaction
  swept and pinned by an e2e (a secret's audit body carries metadata only). The prevention half is a
  fixture guard: console test fixtures must mint uuid-shaped registry ids (`uuidFor`), and converting 25 files
  flushed out four more live uuid-vs-name joins (the Systems standard picker, RoleEditor, AlarmsPanel,
  CapabilitiesPanel), fixed in the same sweep. The `uuid-as-address` anti-pattern is retired in the adversarial
  catalog. Run as the third feature loop on the ADR-0074 machinery.
- **The docs corpus restructures around the design fence ([#434](https://github.com/hyperscaleav/omniglass/issues/434)).**
  A `:::design` container marks unbuilt prose structurally: it renders as a dashed
  target-design aside, must name the issue or ADR that tracks its gap (an unowned fence
  fails the build), and is machine-readable (`internal/docslint`'s `Regions`). The thirty
  hand-written "Still Design" summaries across fourteen pages migrated onto fences at the
  sections they described; the cascade and the two-lane data plane each consolidated to
  one home (cascade.md, messaging.md); the six entirely-unbuilt pages moved to a Design
  sketches sidebar group with page-spanning fences; this build log split off status.mdx
  (which dropped from 2,300 lines to 80); the glossary reconciled term-by-term against the
  generated schema; and a badge-fence lint now fails `make test` when any architecture
  page's badge disagrees with its fence census, so the badge is derivable, not asserted.
- **The drift-prevention gates land: a documented fact must exist ([#429](https://github.com/hyperscaleav/omniglass/issues/429)).**
  Seven slices close the loop the 2026-07-30 audit opened. `make test` now fails when
  current-tense prose names a route absent from the OpenAPI document (methods checked,
  fence-aware, with an illustrative-marker escape), a permission that gates no route
  (both directions: doc mentions and seeded grants, matched with the real rbac rules
  minus the read floor, handler-enforced admin tiers registered against their call
  sites), a make target, env var, or repo path that does not exist (with the generated
  config registry counting as documentation by construction), or a `table.column` the
  generated schema facts do not carry (seeded dotted registry keys exempt). The CLI
  docs guard now validates documented flags against the cobra tree and walks the
  architecture and contributing trees too. The semantic drift no lint can see gets the
  periodic `/docs-audit` skill, encoding the audit's fan-out, drift definition, and
  issues-not-files landing rule.
- **The prose diet: the corpus cut to its claim-preserving floor ([#533](https://github.com/hyperscaleav/omniglass/issues/533)).**
  The architecture corpus (excluding the three logs) drops from 92,228 words to 68,473,
  25.8 percent, with every cut classified: restatements deleted, rationale collapsed onto
  the ADR that records it, duplication sent to its one home, one worked example kept per
  concept, and fenced design tersened to sketch density with each fence's tracker owning
  the depth. Two falsified claims the read surfaced were corrected (the spine's retired
  pin-model invariant, api-first's pluggable-engine claim). The 40 percent aim proved
  unreachable without deleting claims; the acceptance was amended to the measured floor,
  with the no-claim-lost rule outranking the number. The #429 lint suite guarded every
  identifier through both passes.

- **The segment rule reaches every segment-bearing table** (#552). `ValidateEntityName` guarded
  `component`, `system`, and `location` and nothing else, so seven registries accepted whatever they
  were handed. The rule now runs on `location_type`, `standard`, `vendor`, `driver`, `capability`,
  `product`, and `node`, and its vocabulary moved to the settled word: `ValidateSegment`,
  `ErrInvalidSegment`, `ErrSegmentIsUUID`, in `segment.go`. It also moved into the contract, where
  API-first wants it: the create bodies carry `pattern` and `maxLength`, so the generated spec,
  client, CLI, and JSONSchema enforce it too. The rule itself did not change and every seeded row
  already satisfied it.

  An adversarial review pass caught what the first attempt shipped. Five registries route their
  errors through one shared `mapTypeErr`, which never learned the new sentinels, so an operator typo
  returned **500** on `/vendors`, `/drivers`, `/capabilities`, `/standards`, and `/location-types`;
  only the node path had been taught. `product` had been missed outright, and a uuid-named product is
  unreachable by its own handle from the moment it is written, because `registryRefCol` routes a
  uuid-shaped reference to `where id = $1`. Two guards were added rather than two fixes: an API-tier
  test drives every affected route with nine illegal segments and asserts the operator receives a
  422, and `TestEveryNamedTableIsClassified` reads the generated schema so a new named table fails
  the build until somebody classifies it as a segment or a keyspace key. The API-tier test then found
  a **pre-existing** 500 the review had not: `ErrSegmentIsUUID` was mapped only in the advisory
  `:check-name` verb, never in the create mappers, so a pasted uuid 500'd on `/components`,
  `/systems`, and `/locations` before this slice existed.

  Two gaps are recorded rather than closed. No registry patch carries a name field, so the renameable
  handles ADR-0062 describes are unreachable through any update path ([#555](https://github.com/hyperscaleav/omniglass/issues/555)).
  And `principal_group` is guarded by a looser pattern (`^[a-z0-9][a-z0-9._-]*$`) that admits `.` and
  `_`, which the address grammar would read as token separators.

- **One identity cell replaces four column idioms** (#553, #554). A list rendered an entity's identity
  four different ways depending on the page: separate `Name` and `Display name` columns, `Key` and
  `Label` columns, one `Name` column with an inline muted segment, or the segment alone. Sixteen
  hand-written column definitions, and the header word for the same fact differed between them.
  `IdentityCell` and `identityColumn` state the rule once (label on top, segment beneath, suppressed
  when equal, no uuid in a list), matching what `TreeList` already rendered. The keyspace pages keep
  `Key` as their header word and deliberately do not derive, since `icmp.rtt_avg` is a legal key and
  an illegal segment. On the write side, `createIdentity` had three consumers out of twenty surfaces
  despite being built for exactly this; every segment-bearing create form now consumes it. The
  recon that preceded the work corrected the plan: the rule was going to move into `ListShell` and
  `DetailShell`, but `ListShell` is chrome only, `DetailShell` contains no shell (it exports
  `RelatedList` and the filename is a leftover), and `Page` has one consumer in the SPA, so the rule
  had no home to move into and needed a primitive built instead.

- **The detail surfaces caught up with the list** (#553). Unifying the list column exposed that the
  word "Name" then meant two things two clicks apart: a list column header rendering the label, and a
  blade fact rendering the kebab address. Seven blades called the address "Name" while the three
  estate blades already called it "Technical name"; they now all say "Technical name". Five registry
  blade titles rendered the address in the data face, so an operator clicked a row reading "Crestron"
  and got a panel titled "crestron"; the title now reads the same rule the row does. A source guard
  (`identity-vocabulary-guard.test.ts`) pins it, because the failure mode is a new page reaching for
  the wrong word and the page nobody wrote a test for is the page that drifts. Create forms still
  label the address "Name", which is the pre-existing convention on every page and is settled by the
  `display_name` to `name` rename, which moves both words at once.

- **One word for the machine identifier: Key** (#553). The console had settled on `Segment`, which
  named a part where the whole is not visible: on a vendor form there is no path on screen, so the
  word was precise for us and meaningless at the point of use. It also hardened a migration artifact
  into vocabulary, since `Key` and `Segment` were never two concepts but one concept in two states, an
  entity key already kebab and a keyspace key still snake pending the address grammar.

  `internal/key` had fixed the right doctrine long before this slice and nobody had noticed: a segment
  is one dot-separated component of a key. So a key is a value and a segment is a position, which
  makes "segment" correct in prose about topic structure and wrong on a form. The console now says
  `Key` everywhere, `identity-vocabulary-guard.test.ts` retires `Segment` alongside `Display name` and
  `Technical name` as label text (matching quoted strings and JSX text only, so a donut chart may keep
  its segments), and `ValidateSegment` became `ValidateEntityKey` in `entity_key.go`, which no longer
  collides with the `segment` regex `internal/key` already owned.

  Two key rules remain, deliberately: an entity key is kebab and a keyspace key is snake with an
  optional dot hierarchy. They are not merged, because each legitimately carries a character the other
  forbids. The console says `Key` for both, and the differing character set surfaces as a validation
  message rather than as a second word.

- **Every identity exception is named explicitly** (#552). The key classification guard only inspected
  tables carrying a `name` column, so it saw 23 of 51 and was blind to every table identified by
  something else: `human` by a username, `blob` by a sha256, `task` by a content hash. Absence of a
  `name` is not evidence of absence of an identifier. It now declares one of four identity shapes for
  every table (key-bearing, keyspace, a human identifier that is not a key, id-only) and fails on a
  table with none or with more than one, so a new table is a failing test until somebody classifies
  it. Proved non-vacuous in both directions: removing a table fails, and declaring one twice fails.


- **The identity triad, on every surface** ([#545](https://github.com/hyperscaleav/omniglass/issues/545)).
  The vocabulary above is reversed, and this entry records why rather than quietly replacing it. The
  columns have always been `id` / `name` / `display_name`; what drifted was the console, which had been
  taught to call the friendly string the Name and the identifier the Key. Prior art settled it: across
  35 systems in five families, no system names an entity's friendly string a `label`, and `display_name`
  is the blessed standard field in AIP-148, which this API already follows. So the schema does not move
  and every other surface agrees with it ([ADR-0076](/architecture/decisions/)).

  Proving the classification is what exposed how wrong it was. `tag`, `variable`, and `secret` were
  declared keyspace on the strength of the word "key" in their prose; none of them carries a dot, which
  is the only thing the keyspace rule adds, so all three are ordinary entity names. Behind the
  misclassification `tag` had its own private validator, a fourth name rule with its own ceiling and
  error text, and `variable` and `secret` had **no name validation at all**: a secret could be named a
  uuid. `CreateSecret` checked its crypto provider before the name, so an illegal name reported "no
  secret key provider configured". Only `property_type`, `event_type`, and `command_type` are keyspace.

  There is now one validator. `storage.ValidateName(table, name)` picks between the two rules from the
  table's declared identity shape, so a call site cannot pick the wrong rule or forget to pick at all,
  which is how `system_role` had reached production unvalidated. The four superseded validators are
  **deleted**, not renamed: leaving them exported would have let the next registry opt out of the
  primitive while passing review and `make test`. `principal_group`'s looser API-layer pattern folded in
  with it ([ADR-0077](/architecture/decisions/)).

  Renaming became an explicit act. `POST /<collection>/{ref}:rename` is gated by `<resource>:rename`,
  and `name` is removed from the four PATCH bodies, because two ways to rename would have made the
  permission decorative. `audit_log.resource_id` now always holds the primary key: it had held a name
  or the caller's reference depending on the route, so one entity accumulated two audit keys and a
  rename orphaned half its history.

  Three guards carry the invariants, and each reads the tree rather than a hand-written list, because a
  hand-written list is how the last three escapes happened. One checks every `ValidateName` call site
  names a declared table; one checks all 96 `writeAuditRes` sites key on a primary key, as a whitelist,
  because a blacklist waves through the commonest mistake of passing a dual-accept route's `id`; and one
  checks that a console label matches the field it labels, after eleven detail blades were found showing
  the identifier and the friendly string both under the word "Name" while 812 tests were green.

- **The blade field becomes a primitive** ([#574](https://github.com/hyperscaleav/omniglass/issues/574)).
  The blade *shell* was a primitive and the blade *contents* were not, so every blade defect was an
  N-place defect. The identity work paid that three times in one body of work: `display_name` was
  labelled "Name" on 11 blades, a long description failed to wrap in 24 fields, and 3 blade headings
  rendered a raw id. Counting the duplication before designing anything found eleven pages defining a
  byte-identical local `Field` (all eleven bodies hashing the same), a twelfth idiom in three
  create-as-route forms, positional `ctx.field(...)` / `ctx.fact(...)` helpers on four more, and the
  read-only box hand-rolled 24 times.

  Reading the code changed the answer. The label-and-control wrapper already existed as `FieldRow`,
  and it was better than the eleven copies: it associates the label to the control by id with the
  tooltip trigger outside the `<label>`, where the copies wrapped the control in a `<label>` and
  broke its accessible name. Both positional helpers already delegated, to `FieldRow` and
  `KVStacked`. So the duplication was not two implementations, it was one implementation behind five
  call forms plus eleven pages that had adopted none of them. `BladeField` is therefore not a new
  wrapper but the read-or-edit switch composed over the two that existed, and no `BladeFact` was
  added, because `KVStacked` already was one.

  **A read-only field renders as a fact, not a box that refuses typing** ([ADR-0078](/architecture/decisions/)).
  The question was invisible in code and obvious in a screenshot: the seed-owned vendor blade, which
  nobody can edit, rendered five bordered input boxes directly below three plain facts. One
  read-only state, two appearances, and the boxed one signalled an editability that did not exist.
  Making the read state a fact also fixed half of [#573](https://github.com/hyperscaleav/omniglass/issues/573)
  by construction, because text in a fact wraps.

  The identity pairing stopped being a convention and became a type. A field or fact that shows one
  of the two words names the fact it is bound to (`bind="display_name"`) and takes its label from
  `IDENTITY_LABELS`; `label` is refused alongside `bind` on all three components. The guard that had
  enforced this by scanning source shrank accordingly: gone are the four-alternate regex, the
  eight-line lookahead window, its terminator, and the `display()` heuristic, all of which existed
  only because 74 sites paired a label to a binding by hand. What is left is the one failure a type
  cannot catch, a page bypassing the components, and it was mutation-tested three ways before being
  believed, including a multi-line tag the old line-based regex would have missed.

- **A blade heading tracks its row** ([#579](https://github.com/hyperscaleav/omniglass/issues/579)).
  Renaming an entity from its blade updated the row, the list, and the blade body, and left the
  heading showing the old words until the blade was closed and reopened. The cause was one line of
  placement repeated eight times: `const r = row()` in the component body, where a Solid read
  subscribes to nothing because the body runs once. The lookup was correct; only the tracking was
  missing, which is why the [#572](https://github.com/hyperscaleav/omniglass/issues/572) guard, which
  checks that a heading resolves its row at all, passed the whole time.

  `BladeTitle` is now the heading, so the placement lives in one component rather than being retyped,
  and a second guard checks the narrower rule the first one could not see: a `*Title` component may
  not bind its row accessor to a const in its body. Found not by a test but by driving edit, Save,
  and read through a blade while verifying the [#574](https://github.com/hyperscaleav/omniglass/issues/574)
  rollup: both modes screenshotted clean and 834 tests were green, because nothing asserted a heading
  after a write.

- **A blade heading is the words the row showed** ([#581](https://github.com/hyperscaleav/omniglass/issues/581)).
  The third heading bug in the same family, and the one that made the rule structural. Types resolved
  its row correctly and tracked it correctly, and then read the wrong field off it: the name where its
  list showed the display name, so clicking "Machine hall" opened a blade headed `server-room`. The two
  existing guards both passed, because neither is about which field a correctly resolved row is read
  for.

  No regex catches "wrong field", so the third check is membership: a blade title renders through
  `BladeTitle` or is named in the guard with a reason. Writing that list found two more hand-rolled
  headings a manual sweep had missed (the node blade and the effective-property blade), which is the
  argument for the check in one line. The named exceptions are the four entities that carry no display
  name at all, where the name IS the operator-facing string: a secret, a variable, a tag, an interface.
  Verifying that claim corrected it: `display_name` on `SecretType` had been read as `display_name` on
  `Secret`, and a secret has none.


- **Five telemetry lanes, and property stops being the genus**
  ([#584](https://github.com/hyperscaleav/omniglass/issues/584),
  [ADR-0079](/architecture/decisions/#adr-0079-five-telemetry-lanes-and-property-stops-being-the-genus),
  [ADR-0080](/architecture/decisions/#adr-0080-retention-is-provenance-aware-never-declared-never-the-latest-row-per-series)).
  Seven slices on one integration branch, and at the end of them an operator meets five lanes with
  five names and no overlap: a **metric** is a quantity, a **property** is a value, an **event** is
  a typed happening, a **command** is an instruction with a target, and a **log line** is raw
  arrival. Nothing here was greenfield; all five lanes had tables going in, so the epic was a
  catalog split, a fold, a rename, and a narrowing, the shape that hides defects in a large
  mechanical diff.

  In slice order: **one name rule, no dots** (#586) collapsed the two name rules into one single
  kebab token capped at 100 characters, renaming fifteen dotted keys across 83 files with a
  backfill that refuses on collision rather than picking a winner. **The catalog split** (#587)
  partitioned `property_type` on `data_type`, the lane key: numeric rows became `metric_type` with
  the numeric facts (unit, precision), rows kept their ids so every FK repointed as a pure
  constraint swap, and the `kind` column retired along with the `log` value its enum still
  advertised. **The fold** (#591) retired the value store: a declared value is a series row,
  an edit appends, an unset appends a JSON-null tombstone, and every current value is the latest
  series row, derived on read; `PruneSamples` shipped in the same slice as the caller-less
  retention floor (never declared, never the latest row per series, ADR-0080). **The rename**
  (#588) gave the text sample table the bare noun `property` once the store had vacated it, and
  converged the two value columns into one `value jsonb NOT NULL`; the word "state" left the schema
  (a health state is a concept, not a table). **The log split** (#589) narrowed `log_line` to
  component-only with node self-logs moving to `node_log`. **The command lifecycle** (#590) added
  the recorded status (`issued` / `settled` / `failed` / `timed-out`) beside the still-computed
  verdict, the two-armed target across both catalogs, and moved intended lineage onto the command
  itself (`command_id`). **The per-lane wire** (#594) closed the loop: `TelemetryBatch` carries
  `metrics` / `properties` / `events` / `logs` arrays, each validated against its own catalog, the
  polymorphic samples array reserved and gone, with the stored property encoding byte-identical
  across the break so transition detection stayed honest.

  Two mid-loop stops earned their keep. The **wrong-schema stop**: the epic definition had been
  drafted against the pre-July init-dump names (`property` the catalog, `property_value` the
  store), and verifying it against the running database before building found three claims pointing
  at tables that no longer existed under those names; the corrected map carried two rulings, the
  `data_type` partition key and derived-not-stored current values, and the same init-dump trap
  later caught two adversarial reviewers whose reds were refuted by reading the migrated schema
  instead of the dump. The **node_log ruling**: slice D as written would have orphaned the shipped
  self-log lane; the builder hit the stop condition, wrote nothing, and reported with evidence, and
  the architect amended the definition mid-loop to split the store by origin, after which the
  rebuild came back RED-first against the unmodified schema. The docs half landed the model on the
  architecture pages, appended ADR-0079 and ADR-0080, and put the retired vocabulary (`state` as a
  sample table, `property_value`, `StateSampleWrite` and siblings) on the docslint denylist.
- **The catalog surfaces finish**
  ([#601](https://github.com/hyperscaleav/omniglass/issues/601)). The arc-closing epic behind the
  five-lane split: every catalog page names one thing and works for the principals who may see it,
  every capability the lanes landed storage-deep gained its authoring surface, every schema the
  catalogs accept means what it says, and the wire rulings left in an issue body became decision
  log entries. Six slices, one rollup PR.

  In slice order: **the Types split** (#598) broke the one page holding two registries into
  Location Types and Secret Types, fixing the live viewer break (the shared data layer threw when
  the `secret:read`-gated fetch refused, taking the location tab down with it) and renaming the
  `type` permission resource to `location_type` on every surface (ADR-0082). **The metric
  contract surface** (#600) mirrored the property contract for the metric lane: twelve storage
  functions with in-transaction audit rows, three route families with the official-classifier
  refusal, the `ContractEditor` metric lane, the effective-metric resolution reading the metric
  series, and the missing `metric_type_id` indexes. **The command target on either arm** (#596)
  closed the whole projection, not just authoring: the read body, CLI, and console had been
  dropping the metric arm on the floor, and the seed loader had been silently ignoring or NULLing
  targets; both now refuse or carry, the exclusive-arc refusal surfaces as a named 422, and the
  console authors either arm by name through one picker. **Schemas mean what they say** (#595)
  landed the bare-`required` checker at nine storage sites (the mapped six plus three boot
  upserts the recount found) and made `params_schema` enforced at issue time; chasing the
  review's finding exposed the deeper defect, a stored object-valued `additionalProperties`
  arriving as a plain map the validator silently skips, fixed by normalizing it into a real
  sub-schema at parse so enforcement is real. **The guides** (#599) shipped Events and Commands
  admin pages with every claim required true on the branch (four softened where the code lacks
  the feature), and swept the dotted-name copy class wider than its map: console hints, doc tags,
  web comments, and the architecture corpus still teaching the retired two-rule design, with a
  docslint denylist entry so the class cannot return. **The wire contract** (#602) recorded
  ADR-0081: the `og.v1.<verb>.<node>` grammar, the `api.telemetry` own-segment ruling with its
  rejected reserved-name alternative, per-record subjects rejected, and the worklist/heartbeat
  singleton consequence with its HA fork deferred (telemetry does not face it; its durable
  JetStream consumer already joins), with messaging.md gaining the grammar section as
  page-of-record.
- **The catalog finds its sections**
  ([#607](https://github.com/hyperscaleav/omniglass/issues/607)). Fourteen flat Catalog entries
  hid five real clusters; now the rail reads as a taxonomy under one naming rule (ADR-0083): a
  section is named for the estate noun it serves, an entry keeps the registry's own word, and
  where the registry's only word is "type" the entry is Types with the section completing the
  sentence. **The sectioned rail** (#608) renders non-folding headers from the permission-filtered
  entry list (a fully gated section disappears with its entries), tags sectioned palette entries
  `Catalog · <section>`, spells the full address in the top bar, adds the Logs and Notifications
  soon stubs, and walks Templates off the rail while a nav-path binding test pins that every
  unlive entry resolves to a registered stub. **The overview hub** (#609) opens `/catalog` from a
  visible entry: one card per section derived from the same navItems the sidebar renders and
  filtered through the same permission logic, live registry counts through the list pages' shared
  query keys, each registry its own query so one failure marks its own row. Its review retired a
  vacuous leak-pin (a synchronous not-called fetch spy that the client's async middleware made
  unfailable) for a query-cache assertion, and the vacuous-async-spy class joined the
  anti-pattern catalog. **The map** (#610) recorded ADR-0083 with the rejected shapes (flat rail,
  hover flyouts, uniform Types suffix) and the #606 fence around the rules/alarms vocabulary.
  Organizing line, taught on the UI page: Telemetry is what gets recorded, Action is what the
  platform does.
- **The catalog shell**
  ([#619](https://github.com/hyperscaleav/omniglass/issues/619)). The sectioned rail lived
  three days: four design rounds against the live console replaced it with a shell, and the
  catalog IA locked as **direction, not genus** (ADR-0084). Catalog is one rail entry opening a
  two-column area: a grouped subrail (Telemetry, Actions, Components, Systems, Locations,
  Metadata) whose entries navigate to the real per-registry pages rendered in the pane at their
  canonical flat URLs, and an Overview landing of teaching cards; both derive from one group
  table judged through the same permission filter the rail uses. **The shell landed** (#620)
  with every soon slot naming its tracking issue (#615, #616, #617 templates; #618
  notifications; #624 rules), the guides on one path phrasing, and a source-level pin keeping
  the thirteen catalog routes inside the shell block beside a gate-mirror test tying every
  entry's gate to the route guard. **The blade model closed over every field** (#621, the
  approval caveat): the audit of all twelve registry blades found the violations concentrated
  in ContractEditor and RoleEditor, which rendered declare, edit, and withdraw controls in read
  mode; both now consume the hosting blade's edit slot, and leaving edit mode discards open
  drafts, so nothing typed before Cancel resurrects on the next pencil press. **The decision
  landed** (#622): ADR-0084 with the five-signal-lanes noun (four inbound, one outbound; a
  command is an instruction, not a reading), the glossary lane row rewritten, the phrase on the
  docslint denylist, and ADR-0083 superseded. Along the way the shell exposed a latent
  primitive bug (BladeStack's fixed positioning captured by the page shell's filled fade-in
  transform: blades now portal to body) and a screenshot-gate gap (#623: the freshness
  tolerance passed a dead rail at 0.13 percent).
- **The palette finds a registry again**
  ([#628](https://github.com/hyperscaleav/omniglass/issues/628)). Collapsing the catalog rail
  took the registries out of the ⌘K palette with it: its command list was flattened from the
  nav items alone, so once Products, Metrics, and Tags moved off the rail, typing their names
  returned no matches and the only catalog destination left was the word Catalog. The palette
  now builds its list from both surfaces' own tables, the nav items through the same
  `filterNav` that hides a sidebar button and the catalog groups through the same
  `visibleGroups` the subrail and the Overview cards render from, so a registry is findable by
  name without a second membership list to drift, tagged `Catalog · <group>` and searchable by
  its group (`telemetry` lists Metrics, Properties, and Events). Gating came with it: the list
  was previously built at module scope from the unfiltered nav, offering an Audit jump the
  route guard would only bounce, and it is now judged per caller like every other surface. The
  pathless soon slots (the Templates reservations) are no destination and stay out; secret
  types keeps its standing ruling of no nav slot.
- **Every component is an instance of a product**
  ([#614](https://github.com/hyperscaleav/omniglass/issues/614)). The `component_type` registry
  returns as a tree, above the product rather than beside the component: a seeded-plus-custom
  device-class genus (`display`, `mic`, `dsp`, ...) nesting by `parent_id`, carrying the identity
  facts a generated name and a console glyph need (naming stem, icon, abbrev, default tags), each
  inheriting down the tree with override at any node ([ADR-0085](/architecture/decisions/#adr-0085-the-component_type-registry-returns-as-the-device-class-genus),
  a partial reversal of [ADR-0047](/architecture/decisions/#adr-0047-the-fields-fold-product_property-and-property_value)).
  `product.component_type_id` and `component.product_id` both land `NOT NULL`, closed in three
  ordered migrations (a nullable schema step, the boot seed plus a one-time backfill pointing every
  existing row at a matching generic, then the `NOT NULL` floor), so no component ever exists
  unclassified; three generic products (`generic-device`, `generic-app`, `generic-service`) cover
  anything not yet modeled more specifically. `product.kind` narrows to `device | app | service`,
  loses its silent default (required at create instead), and retires `vm`, folded into `app`
  ([ADR-0086](/architecture/decisions/#adr-0086-the-product-classification-floor-and-the-kind-split)).
  The console: the Products form gains a required Type picker over the seeded tree and an optional
  per-SKU icon override (unset resolves the type's own, walking its ancestor chain), the Components
  create form makes Product required with the three generics offered alongside every real SKU, and a
  new Types admin page (Catalog > Components) lists the registry in tree order with a minimal create
  and edit surface (a custom type's tree placement is fixed at create; there is no reparent leg yet).
  Reconciling the ADR-0047 denylist entry that banned `component_type` in docs prose (now current
  vocabulary again, in its reintroduced shape) closed out `internal/docslint`'s one carried red.
- **A role is a typed slot** ([#626](https://github.com/hyperscaleav/omniglass/issues/626),
  `c006c62`). The role assignment guard swaps its capability comparison for a typed-slot one: a
  component fills a role only when its product's `component_type` falls within the subtree of a
  type the role accepts (any type, if none are declared), and, if the role pins specific
  products, only when its product is one of them. Two new join tables,
  `system_role_type` and `system_role_product`, carry the accepted set and the pin; the refusal
  names both parties in operator vocabulary rather than a bare capability gap. The capability
  registry itself and the health rollup's alarm-impact reading are untouched here, staged for
  retirement in the next slice.
- **Capability-gated staffing retires** ([#626](https://github.com/hyperscaleav/omniglass/issues/626),
  `ca78bd3`, `dbfa284`). The `alarm -> alarm_capability -> degradedCapabilities -> role` chain is
  gone, replaced end to end: an active alarm impairs its component's own verdict wholesale, and a
  role counts an occupant as satisfying it only from that verdict, not from a per-named
  capability. Five tables drop (`capability`, `alarm_capability`, `component_capability`,
  `product_capability`, `system_role_capability`) along with everything wired to them: the
  `/capabilities` API and its console pages, `EffectiveCapabilities`, the component-capability
  routes, and the seeded capability registry. The typed-slot guard from the prior slice becomes
  the only assignment check; it plays no further part in health. A same-slice review round found
  `Occupies()` had landed as `Verdict == Healthy`, a stricter threshold than intended that let
  even an info alarm remove a component from every role it filled; `dbfa284` loosened it to the
  spec, `Verdict != Outage` (a merely degraded occupant still occupies, since severity is how
  loudly to page somebody, not a second staffing threshold), and raised every test and doc
  example that had used a warning-severity alarm as its "take this occupant down" trigger to
  critical, the true down case. Recorded as [ADR-0087](/architecture/decisions/#adr-0087-capability-gated-staffing-retires-an-alarm-impairs-its-component-not-a-named-capability),
  which supersedes [ADR-0049](/architecture/decisions/#adr-0049-the-system-role-capability-gated-staffing-and-the-resolved-capability-set)
  and amends [ADR-0050](/architecture/decisions/#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain),
  filed against this slice rather than at the time, a gap this entry closes.
- **A role says how many it will take** ([#626](https://github.com/hyperscaleav/omniglass/issues/626),
  `4925a7d`). Roles gain `capacity` (an optional upper bound above quorum) and `position_labels`
  (per-slot names); both are wholesale-declaration columns, but only `capacity` preserves on an
  edit that omits it: the storage upsert reads `coalesce(excluded.capacity, system_role.capacity)`,
  so a nil `*int` (the API caller left the field out) keeps whatever cap is already declared, a
  server-side guarantee rather than a rule every caller has to remember to uphold. `position_labels`
  carries no such coalesce, replacing wholesale like `quorum`, `accepted_types`, and `impact` (its
  own doc string: omit or empty clears labelling), the same defect `impact` already carried before
  this task's console fix (see below). A component may fill at most one
  role per system: the migration raises and names the offending pairs before adding the
  enforcing index rather than aborting mid-upgrade on an unnamed constraint violation, and
  `AssignRole` pre-checks inside its transaction so the refusal names both the component and the
  role it already holds (409: it depends on what else is currently assigned). Lowering a
  standard-owned role's capacity below what any one conforming system has assigned is refused the
  same way, since each conforming system stages independently. `mapRoleWriteErr` now
  discriminates by constraint name instead of collapsing every `23505` into "a role with this
  name is already declared here": a double-staffing race, a capacity-below-quorum CHECK, and a
  genuine name collision now report distinctly, and the refusal status follows whether the
  problem depends on other rows (409) or the declaration alone (422), recorded in
  [ADR-0087](/architecture/decisions/#adr-0087-capability-gated-staffing-retires-an-alarm-impairs-its-component-not-a-named-capability).
- **A position is a slot you can address** ([#626](https://github.com/hyperscaleav/omniglass/issues/626),
  `dfda3ab`, `4c6012e`). An assignment carries a 1-based position within its role, ordered by
  creation and never renumbered on its own: unassigning leaves a gap, and the next assignment
  refills the lowest free slot rather than growing past capacity. Both resolved reads
  (`EffectiveRoles` and the health rollup) report a role's occupants in position order via a
  correlated subquery rather than a `GROUP BY` plus `DISTINCT`, since the roles query already
  joins types and products and a naive distinct ordering would have hidden that cartesian product
  instead of just losing order. Position uniqueness is a deferrable constraint, not a unique
  index, added after a one-time backfill: a plain index is checked per updated tuple, which would
  raise the moment a single-statement swap lands the first row on the other's slot.
  `SwapPositions` defers it for the rest of its transaction before exchanging two occupants in one
  UPDATE (`POST /systems/{name}/roles/{role}:swapPositions`); it and `AssignRole` both take
  an advisory lock on the `(system, role)` pair, so two concurrent assigns cannot compute the same
  next-free position. The health rollup gains `short` and `spare`, the occupancy-aware
  counterparts of the roles read's health-blind `understaffed` (a role can read fully staffed
  there and still short here, if what it has is down). A same-slice follow-up (`4c6012e`) found
  capacity enforcement had been emergent from the position collision, mislabeling the refusal
  once positions were contiguous and silently overfilling past the declared cap once a gap
  existed below it; added an explicit pre-check (`CapacityFullShortfall`, 409) beside the
  existing double-staffing one, skipped when the component already holds the role so a full
  role's existing occupants stay idempotent, and renamed the swap body fields from `{a, b}` to
  `{position, with}` (the former generated unusable CLI flags).
- **A standard states alternates** ([#626](https://github.com/hyperscaleav/omniglass/issues/626),
  `1f1de87`, `9817058`, `29810a5`). A role can join an exclusive-or group instead of contributing
  to its system unconditionally: `role_choice` is the group, `choice_alternate` one way to
  satisfy it, `system_role.alternate_id` how a role joins one. The pure fold
  (`internal/health.Alternate` / `Choice` / `SystemVerdictWith`) picks each choice's
  best-satisfied alternate by declaration position and folds only its roles, so an all-in-one
  room's satisfied video bar no longer takes the system down over its component-built
  alternate's five unbuilt roles. `alternate_id` is `ON DELETE RESTRICT`, never `SET NULL`:
  deleting an alternate would otherwise silently promote every attached role from conditional to
  mandatory, the same trade already made when a plain `ON DELETE SET NULL` was rejected at schema
  level; `DeleteChoice`/`DeleteAlternate` refuse instead, naming the roles still attached. A
  composite ownership FK closes the cross-arc hole where a role could join a foreign owner's
  alternate, and a same-slice repair closes `system_role`'s own missing owner-arc CHECK (an
  unknown standard or system name used to resolve to NULL and succeed silently, creating an
  ownerless role a second typo would then update; `role_choice` would have inherited the same
  hole). Two review rounds followed. `9817058` found `SetSystemRole` wholesale-replacing
  `alternate_id` on every write with no route ever populating it, so any PUT through the existing
  role routes, including the console's own save, silently detached a role from its alternate; the
  health report also carried the choice-aware verdict beside an ungrouped role list, so a
  satisfied choice's unbuilt alternate read as ordinary impaired roles with nothing marking them
  as not why, closed by `choice`, `alternate` and `active` on `HealthRole`. `29810a5` found the
  boot-seed reconciliation incomplete: it converged declared alternates onto their positions but
  never touched a stored row whose name had dropped out of the declared set, so a rename or a
  drop against an already-seeded database still collided on `choice_alternate_position_key` and
  aborted server boot. The seed now reconciles the stored alternate set to the declared one every
  boot, existing rows included, **deleting** any alternate no longer declared unless a role still
  points at it, in which case the whole call refuses with `ChoiceInUseShortfall` rather than let
  the FK's RESTRICT surface as a bare constraint violation or silently detach a role: refusing to
  boot beats silently detaching a role, the same trade the `ON DELETE RESTRICT` call above already
  made. This is a narrow carve-out from the boot-seed doctrine's usual insert-if-absent rule,
  safe here only because `choice_alternate` has no operator write path and its positions are a
  packed sequence where an orphan actively collides; recorded in
  [ADR-0087](/architecture/decisions/#adr-0087-capability-gated-staffing-retires-an-alarm-impairs-its-component-not-a-named-capability).
- **A role's impact already reached the verdict**
  ([#626](https://github.com/hyperscaleav/omniglass/issues/626)). The epic's claim that
  `system_role.impact` was declared but never consumed, so a short role contributed outage
  regardless, was false against this branch, and this slice ships no code to fix it.
  `Role.Contributes()` (`internal/health/verdict.go`) has exactly two branches, healthy when
  satisfied and `ImpactVerdict(r.Impact)` when impaired, and `SystemVerdict` folds it worst-wins
  into both the recorded outcome and the served one; the chain landed in `5a050e5` (#323), well
  before this epic was written. Task 5's rebuild changed only what makes a component *occupy*
  a slot, across two commits: `feat: capability-gated staffing retires` (`ca78bd3`) first read
  `Occupies()` as `Verdict == Healthy`, a stricter threshold that let even an info alarm remove
  a component from every role it filled, then its own review-round fix, `fix: a degraded
  occupant still satisfies its role` (`dbfa284`), loosened it to the intended `Verdict !=
  Outage`. Neither commit touched `ImpactVerdict`, `Contributes`, or the fold. The one real
  gap in this area was vocabulary, not behavior: `understaffed` (the roles read,
  `Quorum - len(AssignedTo)`) is health-blind assignment arithmetic, while `short` and
  `satisfying` (the health read) are occupancy-aware, so a role whose sole assignee carries a
  critical alarm can report `understaffed: 0` and `short: 1, impaired: true` at the same
  instant, both correct under their own definition. Both doc strings (`internal/api/roles.go`,
  `internal/api/health.go`) and the glossary now say so explicitly, and
  `TestShortAndUnderstaffedDivergeOnCriticalAlarm` (`internal/storage/health_test.go`) pins that
  a critical alarm makes the two figures diverge while a warning does not, since `Occupies()` is
  `Verdict != Outage`. No `fix:` commit: nothing in the chain needed to change.
- **The console reads a system two ways** ([#626](https://github.com/hyperscaleav/omniglass/issues/626),
  the epic's last task). A predating bug shipped first as its own commit:
  `RoleEditor`'s `buildSpec` sent only quorum, display name, and the typed-slot sets, so the
  PUT's wholesale replace silently reset a role's `impact` to `degraded` on any unrelated edit
  (the server defaults an omitted impact), invisible in the console with no warning; capacity
  and `position_labels` had the same gap. Every **system** now carries a **colour of its own**, a
  hue hashed from its uuid (`system_color.ts`, FNV-1a over the WHOLE string, since a uuidv7's
  leading 48 bits are a shared millisecond timestamp within one devseed run and hashing only a
  prefix would land every seeded system at nearly the same hue), stepping past the five bands
  the theme's semantic tokens already claim, rendered through a `.og-system-dot` swatch reusing
  `.tag-pill`'s per-theme lightness and chroma. **The by-role lens** (`RolesPanel`) reads the
  health body alongside the roles read for occupancy-aware short/spare arithmetic in place of
  the health-blind understaffed/assigned figures, marks a down occupant in place, and fixes the
  assign picker offering a component already staffing a different role in the same system,
  which the server refuses with a 409 (a component fills at most one role per system) nobody
  could have anticipated; it now renders disabled, naming the role it already holds. **The
  by-role occupants drag into a new
  order**, wired through the same draggable/onDragStart/onDragOver/onDrop shape `ColumnMenu`
  already uses (no dnd dependency added), decomposed into the server's only reorder primitive
  (a pairwise position swap) by a new pure `swapPath`. **The by-device lens** (`MembersPanel`)
  names the role, if any, each member fills, pivoted client-side from the same roles fetch
  (`roleByComponent`; no reverse route exists). A standing bug closed alongside: `HealthPanel`
  rendered a role impaired without consulting its `active` flag, so a role belonging to a
  choice's losing alternate (#626) showed as an ordinary impairment beside a healthy status
  banner, flatly contradicting it on the seeded huddle-room shape; both derivations now read
  through a new `activeRoles`, with the excluded roles named under their own heading rather than
  dropped. Records [ADR-0087](/architecture/decisions/#adr-0087-capability-gated-staffing-retires-an-alarm-impairs-its-component-not-a-named-capability),
  which supersedes [ADR-0049](/architecture/decisions/#adr-0049-the-system-role-capability-gated-staffing-and-the-resolved-capability-set)
  and amends [ADR-0050](/architecture/decisions/#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain),
  and backfills five build-log entries the epic had carried without one.
- **The console reads a system two ways, review round.** A review pass on the slice above found
  twelve confirmed defects, all fixed. `EffectiveRoleBody` gains `positions`: index-for-index with
  `assigned_to`, each entry's own 1-based position, since drag-to-reorder cannot address a specific
  occupant's slot by assuming index `i` sits at position `i + 1` once an unassign has left a gap
  (#626, never compacted); `swapPath` (the drag decomposition) reads real positions instead. The
  system-colour hue bands were HSL degrees guarding an OKLCH-rendered dot, leaving three of five
  semantic tokens open (about 28% of systems wore a status-coloured dot); recomputed in OKLCH, with
  the fixed-step escape (which clustered on `--color-primary`) replaced by a golden-angle step. Two
  committed D2 diagrams were stale against schema this epic's own earlier tasks changed
  (`data-model-0.svg` still drew the five dropped capability tables and omitted every table this
  epic added) or hand-patched without their generated geometry (`index-1.svg`'s corrected label
  text shipped without the mask and position a d2 SVG computes from it, so the edge line struck
  through the relabelled word); both regenerated. `RolesPanel` reintroduced the exact
  active-flag contradiction the slice above had just fixed on `HealthPanel` one panel over, and
  went one step further, discarding cache invalidation on a failed write (leaving a partial
  multi-swap reorder undetectable) and never invalidating the health read at all on any write
  (freezing its own short/spare/impact badges, now its primary readout, at their pre-write values).
  `api.md`'s roles section, silent on `capacity`, `position_labels`, `positions`, and
  `:swapPositions`, and still calling every assignment refusal "a 422" when a component
  double-staffing another role or a role at capacity are both 409, is corrected to state the same
  409-versus-422 rule ADR-0087 does. `storage.md`'s three-buckets section is where ADR-0087's
  boot-seed carve-out belongs and had never landed; added there (and mirrored in `CLAUDE.md` and
  the `storage-schema-change` skill) rather than existing only in the ADR and this file.
- **Every gateway read that could resolve a name twice now resolves it once and binds the uuid**
  ([#627](https://github.com/hyperscaleav/omniglass/issues/627) Task 10, ahead of the placement-scoped
  uniqueness DDL below). A first pass (`840066a`) converted roughly 40 inline scalar name subqueries
  gateway-wide to resolve a reference once via `scopedByName` and bind the resulting uuid. A controller
  trace rejected that pass's own deferral of the collection ingest write path: `current_values.go`,
  `settlement.go`, and `property_samples.go` still resolved a component, system, or location by an
  inline name subquery on the hot path, which the moment two rows shared a name would raise SQLSTATE
  21000 or silently fail a CHECK constraint; the same fix round also found `EffectiveProperties` and
  `EffectiveMetrics` resolving their owner inside a CTE rather than a scalar subquery, which never
  raises 21000 at all, it silently joins rows from every same-named owner into one answer, and a
  `CreateSystem`/`UpdateSystem` recompute passing a system's own freshly-written name, with its id
  already in hand, into the health rollup (`d64b314`). A 30-agent adversarial sweep then found six
  more of the same class the first two passes had missed: the node ingest lane discarding a task's own
  component uuid for a re-derived name at four downstream sinks (`cab918e`), the push telemetry route
  authorizing by id then publishing `comp.Name` (`dd92017`), five role-choice writes binding a raw name
  into a scalar subquery (`47864bc`), a standard-owned role recompute re-deriving conforming systems'
  ids from names and raising an unmapped `ErrAmbiguousName` that rolled back the whole write rather
  than failing usefully (`4707bcb`), the command and reconciliation routes re-deriving an id from a
  name the gateway had just resolved moments earlier (`63b715e`), and effective tags' own `?system=`
  filter repeating the CTE silent-union shape in its `seed_sys` clause (`7802b32`). A later,
  independent hunt, told to ignore all prior analysis, wrote a scanner over every SQL string literal in
  the tree and verified closure rather than finding anything new: only four production sites still
  resolve by bare-name scalar subquery, all four verified safe (the three `*NameTaken` advisories, and
  `roleOwnerExpr`'s standard branch, whose caller pre-resolves the id). No caller-observable behaviour
  changed throughout: every existing fixture used globally-distinct names, so the whole suite stayed
  green, and the new duplicate-name tests (`b84a398`, `d889bbc`, `839d4d1`) are the only regression net
  this class of bug now has. Filed [#641](https://github.com/hyperscaleav/omniglass/issues/641):
  effective tags' `?system=` filter still resolves outside the caller's own read scope, narrowed by the
  uuid bind but not closed, deliberately left for a later slice since this task's own contract was to
  change no observable behaviour.
- **A location, system, or component name is unique within its placement, not the whole estate**
  ([#627](https://github.com/hyperscaleav/omniglass/issues/627) Task 11, `f132a80`, `87207b7`,
  [ADR-0089](/architecture/decisions/#adr-0089-a-uuid-is-the-address-a-dotted-path-is-a-positional-lookup)).
  Each of the three tables trades its global `UNIQUE (name)` constraint for a set of partial unique
  indexes, one per placement bucket, plus a plain btree for the ambiguity scan a bare-name resolve now
  has to run. `component` and `system` each carry three buckets (parented, located-but-unparented,
  orphan/root); `location`, which carries no `location_id` column of its own, carries two (parented,
  root). A bare name matching more than one row in the caller's scope is a `409` naming the candidates. A review round (fix round 1: `f55daa2`,
  `829dc6e`, `9e3c017`, `0e4aea0`, `c097dd6`, the last also rekeying the console's tree builders
  (Components, Systems, Locations, the placement pickers) off the now-ambiguous name onto uuid) closed
  its own findings but introduced two new criticals in doing so: a cross-tier scope check that denied
  every non-all-scoped caller on a write carrying a location or system binding, and a scoped
  `component:read` caller's effective-tags system band silently vanishing, neither caught by a fresh
  full test run because no existing test covered a scoped non-all principal on a write path carrying a
  cross-tier binding. The next round fixed both by a byte-identical revert to the pre-task baseline
  rather than a new authorization posture, with two tests genuinely red against the introducing round
  closing the coverage hole that had let them ship (`ba8a9e6`). The same round converged three
  near-identical scope-resolution functions (`scopedByName` / `scopedByNameInScope` /
  `resolveScopedRef`) into one, `resolveRef`, with a policy axis for the read-versus-write disclosure
  split, on the ruling that the converged primitive, checking its caller-supplied scope against the
  resource it was actually resolved for, would have caught both criticals at the first scoped test
  rather than after (`1cf8d74`, `9de2fe7`). A forward-insurance tier guard landed on the same
  primitive, documented as unable to fire on any input the current code produces (`48db30f`). Six
  known edges this convergence ships with, stated rather than hidden, are recorded in
  [ADR-0089](/architecture/decisions/#adr-0089-a-uuid-is-the-address-a-dotted-path-is-a-positional-lookup).
- **A dotted address resolves to a uuid, beside the existing uuid-or-name dual accept**
  ([#627](https://github.com/hyperscaleav/omniglass/issues/627) Task 12, `72147c1`, `87e170d`,
  `3773fe1`, `e09e7b0`). `boi.17c.415a.$comp.display-1` parses to a location-tree walk plus a plane
  switch (`$comp` / `$sys`, `$role` reserved but unresolved) plus a plane-local descent, every segment
  validated against the entity name rule before it reaches a query; a percent-encoded slash arrives at
  the handler already decoded, closing the smuggling path a preflight probe had found. A registry
  whose names are a single global token (`node`, `tag`, the lane types) refuses a dotted ref up front
  rather than 404ing silently (`ec12bf2`). A review pass found the resolved address's not-found case
  reaching a create's body-reference field as the wrong status (a `404` naming the entity being
  `PATCH`ed rather than the missing reference); fixed by folding it to the entity's own sentinel one
  hop upstream of the generic path-param mapper, closing the same defect shape at every other
  body-reference site for free (`a71a549`). The route census was hand-derived twice and wrong both
  times before the third count (82 operations, 57 tree-addressable, 25 single-token) was verified
  against the registration functions rather than a literal-string grep: the tag-binding routes are
  built by string concatenation, invisible to grep, the second time on this branch a route-builder
  helper defeated a coverage sweep the same way.
  [ADR-0089](/architecture/decisions/#adr-0089-a-uuid-is-the-address-a-dotted-path-is-a-positional-lookup)
  records the address grammar and its known edges; this slice is what makes it real.
- **A placement change is its own gated act, `:move`, not a `PATCH` field**
  ([#627](https://github.com/hyperscaleav/omniglass/issues/627) Task 13, `b4449f3`,
  [ADR-0088](/architecture/decisions/#adr-0088-a-placement-change-is-an-authorization-act-so-a-move-is-its-own-verb)).
  `parent` and `location` leave the component, system, and location `PATCH` bodies entirely, a
  **wire-breaking** change the rolled-up PR title carries a `BREAKING CHANGE:` footer for. `:move`
  writes its own audit verb rather than the generic `update`, recomputes a platform-owned generated
  name at both ends of a component or system move, and closes a real gap the split surfaced: the old
  `PATCH`'s reparent branch only guarded a non-empty parent, so an explicit empty string cleared
  `parent_id` to root with no scope check at all, letting a scope-limited principal walk a row out of
  every subtree their grant ever covered; `MoveComponent` / `MoveSystem` now require the same
  all-scoped grant creating a root already needed. `MoveLocation` deliberately gains no matching
  clear-to-root capability, since none existed before this split either. A review pass found the
  console still `PATCH`ing `parent` through the now-`additionalProperties:false` body, `422`ing an
  entire location save over an unrelated field; fixed with tests that assert HTTP method and path and
  throw on any unexpected request, the shape that would have caught the original bug where three tests
  mocking `fetch` at the body level had not (`1382567`). Filed
  [#642](https://github.com/hyperscaleav/omniglass/issues/642): a location move leaves both the old and
  new ancestor chains' recorded health verdicts stale, a pre-existing gap this split carries forward
  rather than introduces or closes.
- **A component's name can generate itself from its type**
  ([#627](https://github.com/hyperscaleav/omniglass/issues/627) Task 14, `3a571f1`,
  [ADR-0090](/architecture/decisions/#adr-0090-a-derived-value-is-a-default-that-tracks-until-touched)).
  Left blank on create, the platform mints `<stem>-<n>` from the resolved `component_type` chain and
  the next free ordinal among siblings sharing that stem in the placement scope, serialized by a
  transaction-scoped advisory lock keyed on stem plus scope rather than a `23505` retry. `name_generated` tracks who holds the pen: true on a
  generated create, false forever once `:rename` touches it, true again only through the new
  `:resetName` (gated by the same `component:rename` token `:rename` uses). A `:move` or a product
  reclassify recomputes a still-platform-owned name in the same transaction as the write that changed
  its inputs; an operator-typed name is never touched by either. A review pass found the generator
  trusted an unvalidated `component_type.stem` (an operator could mint a dotted or spaced stem that
  would either split under the address grammar above or violate the entity name rule outright), fixed
  by validating a stem at `component_type` write time with the same rule as a name (`77febe5`); a
  second pass found the malformed-stem fix had left the **absent** case open, an empty resolved stem
  minting the entity name `-1`, fixed by refusing an empty stem in the generator itself, requiring a
  stem on a root `component_type` (which has no ancestor to inherit one from), and validating the
  generated name as an enforced postcondition rather than an asserted comment (`49b84f6`).
- **A component, system, or location's resolved path, its renders, and the console's own addressing
  all move to uuid** ([#627](https://github.com/hyperscaleav/omniglass/issues/627) Task 15, `c381060`,
  `cc8918b`, `280f95f`, `cb164ad`). A `GET` / `LIST` response now carries `path`, `path_segments`, and
  two display-only `renders` (`dash`, the accessor stripped; `bare`, the final segment further
  compacted to the component's `component_type.abbrev`), computed by walking the entity's own
  placement, never a system it happens to belong to; a `LIST` skips the extra queries the bare render's
  abbrev needs, since no console surface reads it there. The console's URL, every panel, and every
  write on a component, system, and location detail page now address by `id`, ending the bare-name
  addressing this epic exists to retire, with a disambiguation chooser offered when a search matches
  more than one same-named row. A review pass found the chooser pointed at a dead end: every panel on
  the detail page it opened still addressed by name, so the exact case the epic legalizes, two
  same-named components in different rooms, landed an operator on a page where every write `409`d
  (`bfdd7c8`); traced and fixed panel by panel to the wire. A second review pass found three more
  swapped-value consumers the edit sites had not been traced past (a raw uuid painted in a field
  labelled "Component", a reachability-panel cache key still built from the old name, a systems-list
  health badge still keyed on name against panels now invalidating by uuid), the fourth time on this
  branch that enumerating consumers from the edit site rather than the value's type and usage missed a
  real one (`2330954`, `01e4693`). Fixing the assign and add pickers to submit a uuid, above, closed
  staffing but left an asymmetry open on the way out: unassigning a role or removing a member still
  addressed the component by name through `UnassignRole` / `resolveMembershipEnds`, both resolving
  estate-wide via `scopedByName`, so an operator could staff a role or add a member with a
  duplicate-named component and then not undo it. A new `loadByRefWithin` primitive resolves within the
  relation actually being edited (this role's occupants, this system's members) instead of the whole
  estate, closing the gap with no wire change (`5a97266`, closing
  [#645](https://github.com/hyperscaleav/omniglass/issues/645)). `render_test.go`'s ten cases were
  found to exercise a test-local reimplementation of the segment shape rather than `PathOf` itself, the
  fourth occurrence on this branch of a test built on a reimplementation of the thing it is meant to
  catch; rewritten to assert against real fixture rows (`79c2ccb`).
- **A list resolves every row's path in a constant number of queries**
  ([#643](https://github.com/hyperscaleav/omniglass/issues/643)). The path attach the entry above
  shipped ran `PathOf` once per row, two or three queries each, so an unpaginated `GET /components`,
  `/systems` or `/locations` cost tens of thousands of sequential round trips at estate scale and
  re-paid them on every write through the console's query invalidation. A new `PathsOf` walks a whole
  page at once: the same recursive CTEs, the same `CYCLE` guard, seeded with every id and carrying the
  seed through the recursion as `origin`, so a page costs three queries for the two accessor planes
  (the rows' own chains, their plane roots, and the distinct plane-root locations) and one for
  locations, whatever the page size. `PathOf` stays, as the single-row entry point and as the oracle
  the new equivalence test holds the batch walker to row for row. The `scopedConfig` hook became a
  batch one (`attachPaths`), taking a slice rather than a row, so `GET` and `LIST` render through the
  identical code instead of drifting apart down two paths; the bare render's abbrev lookup, still
  `LIST`-skipped, now resolves once per distinct product rather than once per row. No wire change:
  `path`, `path_segments` and both renders are byte-identical to what the per-row walk produced.
- **The test harness migrates once per binary, not once per test**
  ([#649](https://github.com/hyperscaleav/omniglass/issues/649)). `storagetest.NewDSN` created a
  database per test and then replayed the entire migration chain into it, so a run of
  `internal/storage` applied roughly 6,600 migrations and spent most of its wall clock in setup:
  the per-test distribution was not a spread of test costs but a spike sitting on a fixed
  provisioning floor. The chain now runs once per test binary into a template database, and each
  test is provisioned with `CREATE DATABASE ... TEMPLATE`, a file-level copy. `schema_migrations`
  is copied with everything else, so a provisioned database stays indistinguishable from a
  migrated one, including to dbmate, and the rollback tests that drive dbmate against a harness
  database are unaffected. Isolation is unchanged and now has a test of its own: a row written in
  one database is absent from the next one provisioned, which also proves nothing leaks into the
  template. Postgres refuses to copy a template that has live connections, and `internal/migrate`
  builds a `dbmate.DB` it never explicitly closes, so the template is sealed rather than assumed
  clean: stray backends are terminated and `allow_connections` is set false, making the copy
  deterministic instead of timing-dependent (a retry on the in-use error would have converted a
  deterministic bug into an intermittent one). Review caught the one regression the speedup
  smuggled in: per-binary work behind a `sync.Once` caches its error permanently, so a single
  transient container hiccup failed every remaining test in the binary where the per-test replay it
  replaced would have cost exactly one test. Success is cached and failure is not, so the next test
  retries. `internal/storage` fell from 152s to 65s (`b661c4b`, `2a02307`).
- **The bare render compacts only a name its own type minted**
  ([#654](https://github.com/hyperscaleav/omniglass/issues/654)). `RenderBare` replaced any final
  segment ending in a digit run with the component type's `abbrev`, without checking the segment was
  one that type generated. So a component an operator renamed to `rack-3` rendered as `dsp3`, putting
  a word on a cable label that appears nowhere in the entity's name, and any hand-chosen name ending
  in digits reproduced it (`booth-2`, `row-14`). The code had drifted from its own documented design:
  both [core entities](/architecture/core-entities/) and the wire description already said the
  substitution swaps the abbrev in for the segment's **stem**. The stem was in hand the whole time,
  since `resolveTypeFacts` returns it beside the abbrev and the path attach discarded it; both now
  resolve and memoize together per distinct product, and the substitution fires only on an exact
  `<stem>-<ordinal>`. The regex that scraped a trailing ordinal out of a whole segment is gone, which
  is the first piece of the ordinal-as-a-stored-fact work
  ([#657](https://github.com/hyperscaleav/omniglass/issues/657)) landing early.
- **Two console papercuts from the identity work close**
  ([#644](https://github.com/hyperscaleav/omniglass/issues/644),
  [#646](https://github.com/hyperscaleav/omniglass/issues/646)). The name-availability precheck on
  the component and location edit forms passed the row's parent and location as NAMES, which stopped
  being unique estate-wide when names scoped to placement. With a duplicate-named parent selected the
  request could not say which one was meant, and the `catch` swallowed the error into operator
  silence: no "taken", no "free", no explanation. Both rows already carry the uuid and `:checkName`
  dual-accepts one ([ADR-0062](/architecture/decisions/)), so the precheck now addresses placement the
  way every other path on those pages already did. Separately, an operator bounced off a filtered
  deep link by a session expiry lost their filter: `AuthGuard` captured `pathname + search` but
  `Login` resolved only the pathname, so `/components?system=<uuid>` came back as the unscoped list.
  The capture and the resolve were inverse operations living in two files that disagreed, so they now
  live in one module (`web/src/lib/next.ts`) where a change to either is visibly a change to the pair,
  and the origin check that blocks an open redirect sits with them.
- **A `PATCH` can say which fields it writes, so it can clear one**
  ([#666](https://github.com/hyperscaleav/omniglass/issues/666), with
  [#638](https://github.com/hyperscaleav/omniglass/issues/638),
  [#639](https://github.com/hyperscaleav/omniglass/issues/639),
  [#640](https://github.com/hyperscaleav/omniglass/issues/640)). The API could set a field and narrow
  one, but for anything that was not a string it could never unset one: `system_role.capacity` is an
  integer, preserved on omit and with no empty-string sentinel available, so once an operator set a cap
  raw SQL was the only way back to unbounded. `internal/updatemask` is the primitive that closes it, a
  pure `Resolve` over three lists with AIP-134's rules exactly and unit tests written as the
  specification of them: an absent mask is the implied mask of the fields the body populated (what
  every `PATCH` in the tree already did, which is why none of the other 108 registrations change or
  need to), a present mask writes exactly the fields it names so one carrying its zero value clears,
  `["*"]` is full replacement and refuses to be combined with named fields, and a mask naming a field
  the resource does not patch is a 422 naming the field rather than a silent no-op. It rides in the
  request body, not the query string ([ADR-0091](/architecture/decisions/#adr-0091-an-update_mask-says-which-fields-a-patch-writes)),
  and generates into OpenAPI, the typed client and a CLI flag with no hand editing.

  The role declarations are its first consumer and the reason it is provably a primitive rather than a
  helper: they were a `PUT` carrying two semantics at once, `capacity` and `alternate` preserving on
  omit while everything else replaced wholesale, so a save carrying only a display name silently reset
  the role's `impact` to `degraded` (moving its system's verdict rollup with no visible field and no
  warning) and dropped its position labels and its typed slot. Both arcs, standard-owned and
  system-owned, become `PATCH` and consume the mask; `SetSystemRole` builds its `ON CONFLICT` set list
  from the resolved write set, so a field outside it is read and rewritten from the stored column
  inside the one upsert rather than merged in from a row read a moment earlier. Two wire behaviors
  changed deliberately: an empty list is not a populated field, so `[]` means "unchanged" where it used
  to clear (clearing means naming the field in the mask), and the console's role editor therefore
  declares every field it owns in `update_mask`, and pointedly not `alternate`, which it has no control
  for. A role's `alternate` also gained a read side: it was writable and returned by nothing, so an
  operator could join a role to an alternate and had no API way to confirm it landed; both role reads
  and the write's own echo now carry it as the same `choice/alternate` address the write body takes,
  asserted as a round trip.
- **A location move recomputes both ancestor chains**
  ([#642](https://github.com/hyperscaleav/omniglass/issues/642),
  [ADR-0092](/architecture/decisions/#adr-0092-a-location-move-recomputes-both-ancestor-chains)).
  `MoveLocation` never recomputed health, so a location with placed descendants moving to a new parent
  left the branch it abandoned frozen at the verdict of a room no longer in it, and the branch it
  joined reading healthy over a room that was: the one rollup that genuinely depends on where a row
  sits, since `locationVerdict` folds every system in the location's own subtree. The gap predates the
  `:move` split (`UpdateLocation`'s reparent branch never recomputed either), which is why ADR-0088
  recorded it rather than closing it inside a task whose contract was to change no observable
  behavior. `recomputeMovedLocation` is the trigger, the location-tier twin of `recomputeMovedSystem`:
  it names ONE row per side, the moved location and the parent it left, because `locationsOver`
  already takes its named locations as the seed of a recursive ancestry walk, so each named row
  carries every ancestor above it and walking either chain in Go would reimplement the walk the query
  performs. Resolving the old side after the write is safe for a reason worth stating: the only parent
  edge the write touches is the moved row's own, so the old parent's ancestry reads the same before
  and after. The regression is red on both halves independently, proven by mutation as well as by the
  original RED: dropping the left-behind parent fails only the old chain's assertions, dropping the
  moved location fails only the new chain's, and each side is asserted two levels up so an
  implementation that named the two parents without walking above them fails too. The trigger sweep in
  `health_invariant_test.go` gains a location-move step for the transition-only invariant, which the
  missing trigger never violated (nothing written is never a duplicate), which is precisely why
  nothing turned red on this for as long as it stood.
- **The round-trip counter becomes a primitive, and the list paths are pinned**
  ([#650](https://github.com/hyperscaleav/omniglass/issues/650), out of
  [#643](https://github.com/hyperscaleav/omniglass/issues/643)). The batch address walk shipped with a
  counting querier inlined in its own test file, which made that one file, accidentally, the repo's
  entire performance regression net: no benchmarks, no bench target, no perf workflow. It is now
  `internal/storage/storagetest/querycount`, with tests of its own, and the test that introduced it
  consumes it rather than keeping a copy.

  Promoting it forced the design question the inline version never had to answer. The old wrapper
  wraps the internal two-method querier, which observes the address-attach hooks because they take
  their querier as a parameter, and observes **nothing** on a gateway `List`, which takes a `scope.Set`
  and queries the pool directly. A wrapper handed to one of those would have reported a small, flat,
  entirely fictional number, and every assertion built on it would have passed while measuring nothing.
  So the seam is the pool: `storage.WithQueryTracer` installs a `pgx.QueryTracer` on the connection
  config (legitimate production observability, where an OpenTelemetry tracer attaches, not only a test
  affordance), `NewPG` parses the DSN into a config to reach it, and `storagetest.NewCountingDB` hands
  a test a gateway whose every statement is counted. The primitive's doc comment leads with that
  hazard, because a count means nothing if the code under test does not go through the counted seam.

  Nine list paths now carry an assertion, each checking both flatness across page sizes and an
  absolute ceiling, since equality alone passes a read that is still per-row with a smaller constant
  and a ceiling alone passes one that is flat at a bad number: components, systems and locations at
  four, four and two statements; members at five; alarms at two; interfaces, tags and roles at one.
  Every one of them was proven by mutation, breaking the batch walk, the join and the per-row read in
  turn and watching the specific number move. The fixtures spread their rows across several rooms and
  several depths on purpose: an address walk grows with the distinct rooms a page spans, so a page
  confined to one room makes a per-room loop look exactly as flat as a batch.

  Pointing the instrument at the paths the issue did not name turned up a real one. `ListPrincipals`
  drains its base query and then calls `loadPrincipal` per row, three more statements each (kind
  profile, effective grants, group memberships), so the admin directory reads at 1+3N: four statements
  for one principal, sixty-one for twenty. It is pinned at its current shape rather than fixed here,
  under a test whose name says it pins a defect and whose failure message says the fix is to delete it.
  (Fixed and the pin deleted in [#671](https://github.com/hyperscaleav/omniglass/issues/671), below.)

- **The second performance instrument: benchmarks that measure, and gate nothing**
  ([#651](https://github.com/hyperscaleav/omniglass/issues/651), [ADR-0094](/architecture/decisions/)).
  Counting round trips catches the N+1 exactly and deterministically, and is blind to everything that
  happens inside one statement: a dropped index, a plan flip to a sequential scan, a recursive CTE that
  stops being bounded. `make bench` is the instrument for that blind spot. Ten benchmarks run over the
  real Gateway against real Postgres, at a small estate and a larger one, and none of them runs in
  `make test`, gates a merge, or asserts a duration. A Go benchmark is inert without `-bench`, so
  diagnostic is the natural shape rather than a compromise, and the whole point of the previous slice
  was that a wall-clock threshold on a laptop either catches nothing or flakes until it is muted.

  The sequencing mattered. While per-test migration made provisioning about 90% of a storage run,
  timing anything on top of it would have measured the harness, which is why this waited for the
  template-database copy. The same trap shows up one level down, and the fixtures are shaped to avoid
  it: each estate is built once for the whole binary and shared, never inside a timed loop, and the
  timer resets after the fixture is in hand. The two sizes are not decoration either. One size cannot
  tell a constant apart from a linear cost, which is the entire question: a list should grow with the
  estate, a tag cascade walking one component's ancestors should not, and only the pair says whether
  that is still true. It is: `ResolveTags` reads flat across a tenfold estate while `ListComponents`
  and the batch `EffectiveTags` grow with it.

  One benchmark was built, measured, and deliberately not shipped, and the reasoning is the useful
  part. The health recompute chain (a raise and a clear over a staffed component) costs about 22ms and
  issues 58 statements to do it. Against a measured round-trip floor of about 265us, roughly three
  quarters of that number is transport no query planner can move, and its run-to-run spread is three
  times the reads'. A regression that halved every plan in it would move the total by less than its own
  noise. That benchmark could not fail for the reason it appeared to exist, so it is a comment in
  `bench_test.go` naming the gap instead of a number in the output implying coverage. A path that
  round-trip-bound is counted, not timed.
- **An operator forks a shipped registry row** ([#655](https://github.com/hyperscaleav/omniglass/issues/655),
  [ADR-0095](/architecture/decisions/#adr-0095-an-operator-forks-a-shipped-registry-row-instead-of-the-platform-writing-it),
  the first prerequisite of [#657](https://github.com/hyperscaleav/omniglass/issues/657)). Thirteen
  tables carry `official`, and the enforcement was a flat refusal: `UpdateComponentType` returned
  `ErrTypeOfficial` for any patch to a shipped row. Correct as far as it went, and it left an operator
  nowhere to go. An edit now **forks**: the operator's version of the row lands in `registry_shadow`,
  reads resolve it over the official row, the official row is never written, and restore
  (`POST /component-types/{id}:restore`) discards the fork so later releases reach the row again.

  The key was the decision worth the time, because twelve more registries copy whatever shape this
  slice picks. A `namespace` column with the unique relaxed to `(namespace, name)` was rejected on
  addressing: it makes every name-keyed lookup return two rows with `QueryRow` taking an arbitrary
  one, across helpers shared with the other registries, and it gives one logical row two uuids while
  `product.component_type_id` keeps naming the official one, so the fork would not take effect for the
  rows that matter. Keying the shadow on the shipped row's **own uuid**, in one registry-agnostic
  table, changes only what a read resolves to and never what anything addresses: the name stays
  globally unique, every foreign key and `ON DELETE RESTRICT` survives, and the namespace cannot reach
  a URL. Generic rather than a shadow table per registry because #657 adds columns to the adopters,
  and a typed shadow would double the DDL cost of every future column thirteen times over.

  `component_type` is the first adopter because it is nested, which forces the inheritance question
  rather than deferring it. The answer is **per node, in official structure space**: a shadow carries
  facts (`display_name`, `stem`, `icon`, `abbrev`, `default_tags`) and never structure (the uuid, the
  name, the `official` flag, `parent_id`), so the walk visits the same chain it always did and reads
  each node's effective row on the way. Forking an ancestor reaches every descendant that does not
  override the field; forking a leaf does not cut it off from what it inherits. There is no such
  thing as a forked chain, only forked nodes.

  A fork captures the **whole** mutable row, so a later release's change to an unedited column does
  not reach a forked row, which is the price of taking it over and what restore undoes. Sparse was not
  a live option: null on `stem`/`icon`/`abbrev` already means "inherit from the parent", so a sparse
  overlay would need null to mean two things at once. The resolve still overlays only the keys the
  image carries, which is what makes a column added *after* a fork resolve to its official value.

  Mutation is what made the tests worth having, and it found a live bug on the way: dropping the
  overlay from the inheritance walk left the assertions passing, because `encoding/json` unmarshals
  into an existing non-nil pointer by writing **through** it, so decoding a shadow's stem into the
  official row's `*string` had already rewritten the string that row points at. The overlay now
  decodes into a fresh value and assigns, and each overlay case gets its own row so the test cannot
  hide the same aliasing again. The re-seed acceptance carries the same suspicion: the fixture scuffs
  the official row in SQL before each re-seed and asserts the seed put the shipped value back, so
  "the fork survived" cannot pass by the seed having skipped the row.

  The console reads three origins now, not two: **official**, **custom**, and **overridden**. Edit is
  live on a shipped row for a caller holding `component_type:update` (a viewer still cannot fork what
  they cannot write), the destructive slot carries **Restore shipped** on a forked row and a greyed
  Delete with the official sentence on a pristine one, and the shared registry lock keeps the flat
  read-only verdict for the twelve registries that have not adopted.
- **A system carries a coarse type.** The **`system_type`** registry lands, the system-side counterpart
  of `component_type` and the second prerequisite of the generated-names epic
  ([ADR-0096](/architecture/decisions/#adr-0096-the-system_type-name-returns-as-the-coarse-space-taxonomy)):
  a nested, universally seeded taxonomy of what kind of space a system is, with `system.system_type_id`
  pointing at it (nullable for now; the floor waits until the shipped tree has proven out). The shipped
  tree is `av` over `room` over `board` / `class` / `meeting` / `training` / `conference` / `huddle`,
  and `av` over `sign` over `video-wall` / `interactive-sign`. `stem`, `abbrev`, and `icon` inherit down
  `parent_id` and are overridden at whichever node needs its own, so `board` states `boardroom` and
  `br` while taking its icon from `room`, which overrides `av`'s. Those strings are not filler: they
  are what the naming and label rules will render from, which is why they were chosen here rather than
  left for the rules to invent.

  Using `standard`'s own `parent_standard_id` inheritance for this was considered and rejected. A
  standard fork expresses **design forks**, two ways to build the same kind of room; the coarse
  classifier is a different question, wants a universal shipped seed, and one estate holds ten signage
  standards and six classroom standards under a single coarse type. So the system side ends up exactly
  parallel to the component side, and `standard` is untouched: no columns added, no inheritance
  semantics changed.

  The identifier is deliberately reused. `system_type` was the old column name for what became
  `system.standard_id`, and it sat on the docs-lint denylist; a **table** by that name is a different
  object from the retired column, so the entry is removed rather than exempted, on exactly the
  precedent `component_type` set, and the ADR records the reuse so a reader who greps and finds both
  has somewhere to land. The fossil left in `mapSystemWriteErr` (the constraint name
  `system_system_type_fkey`, which points at the **standard** key) now sits beside
  `system_system_type_id_fkey`, which is the new one, each labelled with which is which.

  The load-bearing test is the inheritance walk, built so it can fail: a three-level fixture with each
  fact set at a different depth (stem at the root, icon at the MIDDLE node, abbrev at the leaf), so
  resolving the leaf has one right answer per fact and each discriminates a different defect. Proven by
  mutation four ways: letting the last non-null win returns the root's icon, refusing to walk returns
  an empty stem, skipping the starting row returns the root's abbrev, and giving `board` its own icon
  in the seed trips the shipped-tree twin's own premise guard. The delete path pre-counts **both**
  reference sides, so a type that still parents another type and a type that still classifies a system
  both come back as the registry's clean in-use 409 rather than whichever raw foreign-key error fired
  first; `deleteTypeRow` became variadic to allow it.

  Surfaces: `GET/POST /system-types` and `PATCH/DELETE /system-types/{id}` under
  `system_type:read|create|update|delete`, the generated CLI verbs, a **System Types** console page
  under Catalog > Systems beside Standards, and a tree-indented type picker on the system create form
  and edit blade. `system.system_type_id` follows the house three-state patch convention, so an
  omitted field leaves the classification alone and an explicit empty string un-classifies.
- **The test harness stops charging a container flake to the gate**
  ([#661](https://github.com/hyperscaleav/omniglass/issues/661),
  [#663](https://github.com/hyperscaleav/omniglass/issues/663)). Two defects in `storagetest`
  container hygiene, shipped together because each fed the other. The container start still sat
  behind a `sync.Once` that stored its error, the exact shape
  [#649](https://github.com/hyperscaleav/omniglass/issues/649) had just removed from the template
  build one layer up, so a single transient start failed every remaining test in the binary with
  the same replayed message at 0.00s each. It now caches success and never failure, behind a mutex,
  and the next test retries. Retrying exposed the second half: `testcontainers` hands the container
  back **alongside** the error when the create succeeded and the wait strategy did not, so the
  caller that only checked the error was dropping a live Postgres, and a retry loop would have
  leaked one per attempt. `startContainer` now reclaims whatever it was handed, and returns a nil
  container on every error path as a contract its callers depend on.

  The other half was three packages, `internal/bus`, `internal/cli` and `internal/node`, that used
  the harness without routing `TestMain` through `storagetest.Main`, leaving cleanup to a reaper
  this box cannot trust. A gate run with `TESTCONTAINERS_RYUK_DISABLED=true` left exactly three
  strays, one per package; the same run after the fix leaves zero, measured both times. The three
  `TestMain` functions are the small part. The load-bearing part is that the requirement stopped
  being a doc comment: `NewDSN` refuses to provision for a binary that is not running under `Main`
  and fails with the fix in the message. The guard sits exactly on the defect, since the call that
  would start an unreclaimed container is the one that fails, in the offending package, and it runs
  ahead of the `-short` skip so `make test-short` catches an omission too. A static walk of the
  importers was the alternative and was rejected: it would have had to parse the module from inside
  a test to answer a question the runtime already knows, and a guard against flaky gate readings
  that can itself flake is not worth having.

  Every claim here is proven by mutation. Caching the start error again fails the recovery
  assertion; dropping the terminate on the failed-start path leaves the injected container
  inspectable; removing `internal/bus`'s `TestMain` turns that package red on its first provision.
  The start failure is injected through a seam rather than provoked, because Docker cannot be asked
  for a half-started container on demand, while the real start and terminate stay covered against
  real Docker by the tests that were already there.
- **The ordinal becomes a stored fact, and the allocator stops parsing names**
  ([#681](https://github.com/hyperscaleav/omniglass/issues/681), the first slice of
  [#657](https://github.com/hyperscaleav/omniglass/issues/657),
  [ADR-0097](/architecture/decisions/#adr-0097-allocation-tests-the-name-it-would-mint-rather-than-reading-the-ordinal-it-stored)).
  The number a generated component name is minted from is written down (`component.ordinal`,
  nullable) instead of being formatted into the name and dropped. `NULL` is the honest state for a
  row the platform did not name, so `:rename` clears it in the same statement it clears
  `name_generated`, and the bare render now reads the column rather than re-deriving an ordinal from
  the string it is about to replace. That finishes what #654 left half-done and turns its guarantee
  structural: a component an operator called `rack-3` has no number to compact, so there is no rule
  left to get wrong, and a hand-typed `display-1`, which the stem match still compacted, is left
  alone too.

  The slice ran first because it carried the epic's riskiest assumption, and the assumption turned
  out to be half right. Storing the ordinal is necessary for everything downstream (the render, a
  label rule's `.Ordinal`, the recompute-and-compare invariant), but it is **not** what unblocks a
  stem-less name, and allocation must not read it. The prefix scan could not count a stem-less
  sibling because its filter was a bare `-` that no name matched, which is a property of parsing
  rather than of storage; inverting the loop to test the name it would **mint** against the bucket
  fixes that on its own, and `mintName("", 1)` is `1`. Reading the column instead would have been
  actively wrong: an operator can type `display-1` by hand, holding the name while owning no
  ordinal, and an allocator that consulted only recorded ordinals would remint it into the
  scoped-name unique index as a `23505` the transaction cannot recover from. Mutating the allocator
  to read only rows with a stored ordinal reproduces that failure exactly, which is now a test.
  Every existing allocator unit test passes unchanged, because the two formulations agree on every
  answer they can both express.

  Six mutations back the assertions: an ordinals-only sibling read, a `:rename` that keeps its
  ordinal, a `:move` that does not re-record it, `max+1` in place of lowest-free, a render that
  ignores the column, and a backfill that fabricates a number for a row it cannot read one off. Two
  of them exposed that the recompute-and-compare invariant was passing for the wrong reason (its
  estate had no row that stayed renamed, and its one move never left its placement bucket), so the
  estate grew both cases before either mutation counted as caught.
- **A label rule renders a label, over a data map that is the sandbox**
  ([#682](https://github.com/hyperscaleav/omniglass/issues/682), the second slice of
  [#657](https://github.com/hyperscaleav/omniglass/issues/657),
  [ADR-0098](/architecture/decisions/#adr-0098-a-label-rule-reads-what-an-entity-is-never-where-it-sits)).
  `display_name` gains the pen `display_name_generated` on component, system and location, the shape
  `name_generated` already had, and a nullable `label_rule` lands on `component_type`, `system_type`,
  `location_type`, `product` and `standard` with the global tier in its own small table. Rules
  resolve most-specific-first and inherit down a nested registry by the same first-non-null walk
  `stem` and `abbrev` follow, per node across the fork boundary, so setting a rule on a shipped type
  forks that row and the official one stays byte-identical.

  The rule language is Go `text/template`, and a custom interpolation DSL was designed and rejected,
  because the security property a label rule needs is not a syntax that cannot express dangerous
  things but an environment that contains nothing dangerous. The environment is a
  `map[string]string` we build key by key: field traversal, a method call and `call` all fail at
  execution over a string, `index` reaches only what the map already holds, an undefined key renders
  as nothing, and a secret is absent structurally rather than filtered. The test that tries to reach
  one pins the key set exactly and then asks for every word a credential might be called, over a
  database holding a real sealed secret.

  What the map deliberately omits is placement, and that is the slice's one divergence from how the
  definition's acceptance text could be read. Because the label is stored, every input to a rule is
  something a write path must re-render on, and the keys chosen change on exactly five acts, all of
  them the entity's own: create, rename, move, reclassify, reset. Adding a location's name would add
  a sixth that is somebody else's, staling every label under a renamed room, and the
  recompute-and-compare invariant would be right to fail. The cascade that would make placement facts
  safe belongs with the bulk recompute ([#685](https://github.com/hyperscaleav/omniglass/issues/685)),
  which can add the keys and the cascade in one piece.

  The global tier is one row per entity kind with **two** columns, `default_template` for what the
  release ships and `template` for what the operator chose, because one column cannot be both
  authoritatively re-seeded and operator-owned. The location kind ships an empty rule on purpose: a
  campus, a building and a room have real-world names, and every fact a rule could reach is either
  the type ("Room", for every room) or the name itself, so any rule that ran would be a constant or a
  restatement, and the read ladder's fallback to the name is already the better answer. That is the
  same argument the epic makes for refusing to auto-name a nominal location type.

  Twenty-three mutations back the assertions, and two of them were surviving at first for the reason
  slice 1 warned about. The empty-render test asserted a `NULL` column after a create whose rule
  produced nothing, but the stamp skips a write when the render equals what is stored, so nothing was
  written and the assertion held whatever the write would have done; it now drives a labelled row
  into a rule that stops producing one. And the recompute-and-compare invariant did not notice a
  `:move` that fails to re-render, because its fixture moved a component into a bucket where the same
  ordinal was free, so name, number and label were all unchanged; the bucket is now stocked to force
  a re-mint, and its reset is preceded by a rename for the same reason.
  Review found four things after the slice was first proposed, and one was a hole in the argument
  rather than a bug in the code. **A ceiling on output is not a ceiling on work.** The rendered-label
  cap bounds bytes reaching the writer, and a value built inside a pipeline is materialized by `fmt`
  and never written, so the cap never sees it: 437 bytes of operator-authored rule allocates 85 MB
  and writes 8, every further doubling is 35 more bytes of rule for twice the memory, and twenty of
  them is an unrecoverable OOM of the single binary that re-triggers on every later write to any
  entity of that type, because the rule is stored and storing it on a shipped type forks that row for
  everything under it. Refusing `:=` does not fix it (the nested-pipeline form needs no assignment)
  and neither does removing a FuncMap entry (`printf` is a builtin the FuncMap never granted). The
  fix is the closed-data-map argument applied to the other half of the sandbox: an **allowlist over
  the parsed tree**, a closed set of node types and function names, refused at parse time so nothing
  is ever stored. Six mutations back it, including a forbidden call in non-first argument position
  behind an allowed function, which is the form a checker that inspects only the outermost call would
  wave through.

  The other three were the same defect in three places, a claim with nothing proving it. The pen
  defaulted false with no backfill, so on an upgraded estate the feature would have applied only to
  rows created afterwards; three of the four non-component stamps could be deleted with the suite
  green, because every rename test ran against rows whose rule read nothing a rename changes and the
  recompute-and-compare invariant iterated components only; and `label_rule` was declared
  `ShapeIDOnly`, which promises a uuid, against a text primary key with no id column, published into
  a generated reference. All three now have a test that fails when the thing is removed, and the
  shape guard was extended from "every table has a declaration" to "the declaration agrees with the
  primary key", which is what makes the third one catchable at all.
- **The console reads one label**
  ([#683](https://github.com/hyperscaleav/omniglass/issues/683), the third slice of
  [#657](https://github.com/hyperscaleav/omniglass/issues/657)). Slice 2 stored the pen and never put
  it on the wire, so the slice starts with an API change rather than a console sweep:
  `display_name_generated` joins the component, system and location read bodies, read-only for the
  same reason `name_generated` is, and the operator claims or surrenders it by writing the label
  rather than by asserting a boolean.

  `hasDisplayName` decided whether a row shows its name on a second line, and it decided it by
  comparing the label to the name. That was the same question as "did a human choose this" only while
  a label was only ever operator-typed. A rule-rendered label differs from the name exactly as an
  operator's does, so unchanged it would have put a second identifier line under every row in a
  15,000-component estate. It now reads the pen, with the string comparison kept as the second half of
  a conjunction, because a rule with nothing to say about a row keeps the pen and stores no label
  ([ADR-0098](/architecture/decisions/#adr-0098-a-label-rule-reads-what-an-entity-is-never-where-it-sits)):
  holding the pen does not imply there is a label to attribute. `labelGenerated` is the other half and
  marks the row, and `labelIsName` is the face question `BladeTitle` had been asking by hand.

  Forty call sites were hand-rolling the renderer beside it, in three dialects, and the classification
  mattered more than the count. Twenty-nine were labels. Two were **sort keys**, where converting
  changes ordering, and three were `<option>` and picker rows where the **value** is submitted and the
  text beside it is not: every value was left exactly as it was, which is the [#644](https://github.com/hyperscaleav/omniglass/issues/644)
  defect class this slice was warned about. Fifteen were **filter facets**, and those were the ones
  already broken: a facet spelling its haystack `` `${r.name} ${r.display_name}` `` searched the
  literal text "undefined" on every unlabelled row, so typing "undefined" matched them all and typing
  a label matched none. Two role pickers fell back to a **uuid** rather than the role's name, and two
  registry lookups rendered the empty string for a found row with a blank label, because `??` does not
  treat `""` as absent.

  The sweep is held by a source guard rather than by page tests, for the reason the vocabulary guard
  exists: the page nobody wrote a test for is the page that drifts, and this rule had drifted in forty
  places. It is line-precise, not file-precise, since a file-level exemption is how half a sweep hides
  behind a neighbour that had a good reason (`lib/principals.ts` is exactly that file: its role facet
  goes through the primitive and `principalName` beneath it does not). Four exceptions survive, each a
  rule that is genuinely not the entity label's, and each asserted to still match so a stale entry
  fails too.

  The command palette does not follow, and that is the finding rather than an omission: it is a
  navigation jumper over static rail and catalog labels and holds no entity at all, so there is
  nothing in it to render through the primitive. Finding an entity by its label is a surface that does
  not exist yet.
- **The acronym list**
  ([#684](https://github.com/hyperscaleav/omniglass/issues/684), the fourth slice of
  [#657](https://github.com/hyperscaleav/omniglass/issues/657)). The word "dictionary" is what this
  slice is named after and the smaller half of what it did. `title` upper-cases a word's first letter,
  so `dsp` rendered `Dsp`, and fixing that is a list. Making the list something an operator can edit
  is what turned a package-level constant into a value with a lifetime, and that is the part slices 5
  and 7 consume.

  The settings engine grew **list support** first, which was four separate places rather than one: a
  `default` tag that has to become a typed slice, a write validator that has to admit a JSON array, a
  merge that replaces rather than merges, and a typed decode that has to turn `[]any` back into
  `[]string`. Only the first needed code. The comma-separated tag form is Huma's, followed exactly so
  one tag cannot produce two disagreeing defaults, and the JSON-array spelling Huma also accepts is
  refused loudly here rather than parsed a second way. `label` is the first `platform` namespace and
  the first `platform,client` one: admin-write because a label is stored once and read by everybody,
  client-readable because the console renders one from the same list the server did.

  The engine's lifecycle is the decision worth reading
  ([ADR-0099](/architecture/decisions/#adr-0099-the-acronym-list-is-one-replaceable-setting-not-a-shipped-list-plus-operator-additions)).
  Parsing binds a template's FuncMap, so a compiled rule carries the dictionary it was parsed
  against, and a mutable engine would leave rules rendering from a dictionary nobody can see. A
  change therefore builds a **replacement**, cached against the dictionary itself rather than a
  generation counter, because a counter has a failure mode a content key cannot have: the second
  write path that forgets to bump it. The gateway resolves the setting when it renders, so an
  operator's edit reaches the next write with no restart, and the render functions take an engine so
  the bulk recompute can resolve one for fifteen thousand rows instead of fifteen thousand.

  One defect found by reading rather than by a failing test, and worth the slice's most careful test.
  The dictionary read was issued against the pool from inside the transaction that was stamping the
  row, so it took a SECOND connection while holding one. A pool whose connections are all held by
  writers each waiting for a second connection is deadlocked, not slow, and a bulk import of the
  estate sizes this epic exists for is enough to reach it. The resolver now takes an override level
  the caller read on its own transaction, and a pool of exactly one connection is the whole
  population of that race, so the regression test reaches the deadlock deterministically rather than
  hoping for it under load.

  Splitting parse from render is what kept the ripple small. `label.New` binds the same four function
  names whatever dictionary it holds and the grammar check walks a static allowlist, so whether a
  rule parses is a fact about the rule alone: the ten rule-validation call sites keep a
  dictionary-less engine and only the three render paths carry the current one.

  The console needed the work nobody scoped: it renders every setting through `String(value)` and
  patches back what it read, which for a list is the joined string and a 422 the server is right to
  give. A list now reads as one comma-separated line and writes as an array, decided from the
  generated `"type": "array"` rather than from the value's shape, so a list that resolved empty is
  still edited as a list.

  The honest limit is in the docs rather than left to be discovered: vendor model numbers are
  unbounded, so this list degrades quietly and forever. That is acceptable for a fallback one click
  from being overridden on the row, and it is why the dictionary matters less than it first appeared:
  a type's `display_name` already carries correct casing, and the ladder reads that before it ever
  re-cases a raw name.
- **A rule change previews then applies, and a label can read where it sits**
  ([#685](https://github.com/hyperscaleav/omniglass/issues/685), the fifth slice of
  [#657](https://github.com/hyperscaleav/omniglass/issues/657),
  [ADR-0100](/architecture/decisions/#adr-0100-a-label-cascades-where-the-blast-radius-is-a-placement-and-waits-for-the-verb-where-it-is-the-estate)).
  The verb is what the sub-issue is named after and the smaller half of what the slice did. The
  larger half is that placement in the data map made slice 2's write-path completeness argument void,
  and the argument was the load-bearing part: it could enumerate five acts and call the set complete
  precisely because every fact its map held was the labelled row's own.

  So the acts were re-derived from the map rather than inherited, and the derivation turned up more
  than the sub-issue listed. `MoveSystem` had never stamped a label at all, correctly, while a
  system's map carried nothing a move could touch; `LocationLabel` made it a write path. Membership
  is a write path the epic never named: a component reads its PRIMARY system's type, and `AddMember`,
  `RemoveMember`, `SetPrimaryMember` and `AssignRole`'s implicit bind each move which system that is.
  And `CreateComponent` stamped BEFORE binding its membership, so every create naming a system
  rendered against no system and stored the answer.

  Two acts the sub-issue DID list turned out to be vacuous, which is worth recording as a finding
  rather than quietly satisfying. Moving a location restamps nothing: a location's own map carries
  `Name` and `TypeName`, and a component reads the label of the room it is in rather than that room's
  ancestors, so a campus rename is free. The test that pins it fails the day a location gains an
  ancestry fact, which is the honest way to hold a claim that is currently true by construction.

  The line the slice draws is BLAST RADIUS rather than ownership. Bounded by a placement, the estate
  is restamped inside the act's own transaction and is never observably stale. Bounded only by the
  estate (a rule at any tier, a `component_type`'s `display_name`, the acronym list) nothing is
  restamped and the operator applies it, which is the epic's own argument about rules applied
  consistently to everything else that can stale fifteen thousand rows at once.

  A preview is an APPLY THAT ROLLS BACK, and that was a correction the invariant forced. The
  read-only version could not list what the apply changes: recomputing locations moves their labels,
  which stales the components and systems placed at them, and the first implementation shipped an
  estate that a fresh preview immediately reported as stale. Simulating the second hop would have
  been a second implementation of the cascade for the two to drift apart on, so the preview runs the
  apply and rolls back. It is exact by construction, and the cost is stated rather than hidden: a
  preview takes the operation lock and `FOR UPDATE` on the rows it visits.

  D1 is ruled: one audit row for the operation, keyed on the rule (`label_rule`'s primary key is the
  entity kind, the one key a rename cannot orphan), carrying the count and its per-kind split. Per
  entity writes fifteen thousand rows for one click and buys a restatement, since a generated label
  is derived; the health recompute, which cascades across a whole ownership chain and audits nothing,
  is the precedent.

  Cost is measured rather than asserted: the recompute and the location cascade are both flat in row
  count on [#650](https://github.com/hyperscaleav/omniglass/issues/650)'s counting instrument, and a
  placed, system-bound, generated create is pinned at nineteen statements with every one of them
  named, of which exactly one is this slice's. Twenty-nine mutations, with three surviving the first
  pass: the create-ordering mutation and the unknown-kind refusal each survived because a second
  guard covered them, and the API's update-scope wiring survived outright, because every case in that
  file drove an owner whose read and update scopes were both `all` and a handler passing the read
  scope twice was indistinguishable from a correct one.

  The honest limit: the console has no rule editor, so the verb is reachable from the API and the
  generated CLI and nowhere an operator would find it. `location_type` and `system_type` gained
  `label_rule` on the wire here, because without them the routes this slice ships could never find a
  rule an operator had changed, but the global tier still has no route of its own.
- **A system names itself, and the only one of its kind carries no number**
  ([#686](https://github.com/hyperscaleav/omniglass/issues/686), the sixth slice of
  [#657](https://github.com/hyperscaleav/omniglass/issues/657),
  [ADR-0101](/architecture/decisions/#adr-0101-the-first-of-its-stem-in-a-bucket-carries-no-ordinal-and-the-mint-that-says-so-is-the-one-allocation-tests)).
  The pen and both verbs spread from component to system and location; only a system generates,
  because `location_type` carries no stem and the slice that gives it one is
  [#687](https://github.com/hyperscaleav/omniglass/issues/687). A location's `:resetName` therefore
  refuses with the missing fact named rather than reporting a reset it did not perform, which is also
  the test that flips the day the rule lands.

  D3 is ruled: suppress the first ordinal, accept the order dependence, and write the sequence down.
  A room's only boardroom is `boardroom` and the second is `boardroom-2`; deleting the bare one while
  the second survives frees the bare name for the next create, and `boardroom-1` never exists at any
  point. The suppression is a field on a `nameMint` VALUE rather than a change to the shared shape,
  because a component that suppressed at ordinal 1 would rename every generated component that
  already exists, and `pickOrdinal` now takes that same value. That second half is the one that would
  have bitten: a suppressing mint beside an allocator still testing `<stem>-<n>` disagrees on exactly
  ordinal 1, so the second create in a room mints a name the first already holds and dies on the
  scoped-name index. One value passed to both makes the disagreement unrepresentable rather than
  merely unlikely.

  The placement bucket became a value for the same reason. A system has three buckets and a location
  has two, so the location constructor takes no location id at all: pairing a location with the
  three-way shape is not a mistake to avoid at each call site, it is a value that cannot be built.

  The write paths were DERIVED rather than listed, the lesson slice 5 paid for. The mint reads two
  things, the type chain's stem and the placement bucket, which yields create, `:resetName`, both
  arms of `:move`, and the `system_type` half of a reclassify, and it settles two cases a list would
  have missed: un-classifying a platform-named system is refused by the generator itself rather than
  by a branch written for it, and a `stem` edited on a shared registry row is estate-bounded, so it
  does not cascade, which is ADR-0100's line applied to the name side. Both foreign keys agree
  (`ON DELETE RESTRICT`), so no placement moves without one of those acts.

  Review found two defects after the slice was first built, and both came out of suppression rather
  than out of the plumbing. A reclassify was gated on the `system_type` field being PRESENT rather
  than on the classification changing, and the console sends that key on every save so an unclassify
  can clear: an operator editing a label, with a lower ordinal freed by an earlier rename, would have
  had their system renamed under `system:update` with no rename asked for. And the allocation lock
  still carried the stem in its key, which was sound only while the mint was always `<stem>-<n>`:
  stem `wall` at ordinal 2 and stem `wall-2` at ordinal 1 are the same name, so two concurrent
  creates in one room took different locks and one would have died on the scoped-name index. The
  lock now guards the bucket, which is the only partition of the name space a mint cannot cross.

  The honest limit: the system bare render stays unwired although both halves of its substitution now
  exist. `RenderBare` stamps `<abbrev><ordinal>` whenever it is given both, so a suppressed name
  would print `brd1` for a room named `boardroom`, a digit on a physical label that appears nowhere
  in the name. That is a rendering decision, and it is recorded rather than quietly deferred. The
  console is untouched too: the create flows are slice 8's.

- **A location type opts in to naming its own locations, and a rule change renames nothing**
  ([#687](https://github.com/hyperscaleav/omniglass/issues/687), the seventh slice of
  [#657](https://github.com/hyperscaleav/omniglass/issues/657),
  [ADR-0102](/architecture/decisions/#adr-0102-a-name-rule-is-a-declaration-a-type-opts-in-with-and-a-rule-change-renames-nothing)).
  `location_type` gains a nullable `name_rule`, and its presence IS the opt-in: null means an
  operator names every location of that type. Of the four shipped place types only **floor** ships
  with one, and it ships positional, so a floor created with no name is called `1` and the next `2`,
  allocated among that building's own floors. A campus, a building and a room stay operator-named on
  purpose: `17c` is ground truth an operator holds, and generating one would be the platform
  guessing. The verb `:resetName` stops refusing on this tier, which is the test
  [#686](https://github.com/hyperscaleav/omniglass/issues/686) planted for exactly this slice to flip.

  The one design call: the rule is a **declaration**, `{"stem": "...", "bare_first": <bool>}`,
  decoded straight into the `nameMint` the previous slice made the one home of a generated name's
  shape, and deliberately not the `label_rule` template sitting beside it on the same table. A label
  that fails to render degrades to the next rung of the read ladder; a name has no next rung. It is
  `NOT NULL`, it is in a scoped-unique index, and it is what a runbook outside this system stores. So
  the promise #686's acceptance made and nobody had built, that a rule which would mint an illegal
  name is refused when the RULE is edited, is kept here by MINTING from the rule at edit time:
  ordinal 1 and a nine-digit ceiling bound the whole output space, so a rule legal at both ends is
  legal everywhere in it. A template's output could only have been sampled. The expressiveness would
  have bought nothing either: a location rule can read `Name` (circular, it is what the rule
  produces) and `TypeName`, whose slug is the stem.

  Two rulings the slice owed. **A rule change renames nothing**, and there is no name-side recompute
  verb: #685's preview-then-apply is the LABEL cascade, and the asymmetry is the point rather than a
  gap, since a bulk relabel is recoverable and a bulk rename breaks every reference stored outside
  this system. `:resetName` is the deliberate one-row-at-a-time way onto a new rule. And **a
  positional type permitted at root is legal**, allocating `1`, `2` across the estate: the bucket is
  the placement, never the placement and the type, so two positional types under one parent already
  share an ordinal space, and refusing at root would refuse the large instance of a rule that holds
  everywhere. Its real consequence is addressing, not allocation, and the dotted address already
  answers it (`boi.17c.1` resolves where `1` is ambiguous).

  The write paths were derived from the mint's two inputs again, and it paid twice. The reclassify
  guard is the classification CHANGING rather than the field being present, because the console sends
  `location_type` on every save: that is the live defect review caught on the system tier one slice
  ago, refused entry here rather than repeated. And `:move` re-mints only when the parent actually
  moves, which is narrower than the system tier's, where the same re-mint runs on a move that changes
  no bucket; nothing generated a location name before this slice, so there was no existing behavior
  to preserve and the narrow form is the correct one.

  `location` also gains the nullable `ordinal` the previous slice deliberately withheld from it,
  since the writer it was waiting for is this one, and the recompute-and-compare invariant now covers
  all three trees.

- **The dev estate stops naming itself** ([#689](https://github.com/hyperscaleav/omniglass/issues/689),
  the ninth slice of [#657](https://github.com/hyperscaleav/omniglass/issues/657),
  [ADR-0103](/architecture/decisions/#adr-0103-a-positional-name-is-allocation-order-and-the-real-world-designation-is-a-label)).
  Eight slices built a name generator that the one estate anybody actually looks at did not use: every
  fixture row in `internal/devseed` hand-wrote its own name, and seventeen of the twenty-two hand-wrote
  their ancestry into it as well (`hq-west-2-boardroom`, `boardroom-a-panel`), which is the habit
  placement-scoped names retired two slices before this epic began. Now nothing in the estate names
  itself except the rows no rule could name. Its devices, its systems and its floors ask the platform,
  so `make dev` comes up on the generator's own output: three rooms each holding a `display-1`, a
  divisible boardroom whose halves are `boardroom` and `boardroom-2`, and two buildings each with a
  floor called `1`.

  The mechanical crux was that a generated name cannot be the fixture's identity, because the platform
  picks it and the fixture still has to say "put this component in that room" and still has to
  recognise its own rows on the second `make dev` of the morning. So the fixture grows a `key`, local
  to the document, never written to the database, and every reference resolves through it to a row
  **id** rather than a name (an id is a legal reference wherever a name is, ADR-0062, and it has to be
  one here: a bare `display-1` now matches three rows on purpose). Recognising a row on a later run is
  the other half, and it is answered by the fixture's real claim: it does not say "there is a display
  called display-2 in the boardroom", it says "the boardroom holds three displays". A row the fixture
  names is found by name as before; a row the PLATFORM named is found by its position among the
  platform-named rows of the same classification in the same bucket, in the order the generator
  allocated them.

  The ruling the slice owed was the nominal-versus-positional question meeting real data for the first
  time. The West Building's only floor is the building's Level 2, and a positional generator calls it
  `1`, because a positional ordinal is the lowest free number in the bucket and nothing else. The
  divergence is KEPT: a name is an address and a label is what a human reads, and a floor's
  designation is signage the platform has no access to. (Reversed later on the same branch, see
  the floor entry at the end of this log: a designation is not an integer at all.) The estate now ships both cases of one type
  side by side, a floor named `1` labelled Level 1 and a floor named `1` labelled Level 2, which is
  the clearest thing in it. Seeding a Level 1 so the numbers would line up was refused as
  concealment; making `floor` nominal, the plain reversal of ADR-0102, was refused on cost rather than
  principle and is written down as still available.

  The labels moved the same way. A set `display_name` takes the pen, so an estate that set one on
  every row demonstrated the exact opposite of the label rules: components now render theirs (`Display
  1`, `Video Bar 2`) from the shipped rule over the resolved type and the allocated ordinal, and the
  survivors are enumerated with a reason each. The unclassified power conditioner keeps its typed
  label because the rule can only render "Generic Device 1" for a box with no product; the two
  boardroom halves keep theirs because which half is A is a fact about the air wall, and because the
  shipped system rule reads the type and would label both of them alike; every location keeps its own
  because no location rule shipped yet (four of those thirteen pins were released when one did, and
  two more when the floors were named for their designations, see the words and floor entries at the
  end of this log). Two pure fixture tests hold that line by
  key, so a fourteenth typed label is a deliberate edit rather than a quiet one, and the integration
  suite asserts every seeded name and label by value.

- **The create form asks what and where first** ([#688](https://github.com/hyperscaleav/omniglass/issues/688),
  the eighth and last slice of [#657](https://github.com/hyperscaleav/omniglass/issues/657),
  [ADR-0104](/architecture/decisions/#adr-0104-a-create-form-shows-the-name-it-can-know-and-never-mints-one-to-preview-it)).
  Eight slices built a generator the console could not reach. Two of the three estate create forms
  derived the name from the display name and refused to submit without one, so a row created through
  the console arrived operator-named whether the operator meant that or not, and the third could leave
  the field blank but said nothing about what would land there. All three now lead with the
  **classification** and the **placement**, because those are what the naming and labelling rules read,
  and the identity section that follows shows what the platform will call the row.

  What it shows is the SHAPE, and the ruling this slice owed was where to draw that line. The stem
  falls out of the classification the operator has just chosen; the ordinal is the lowest free number
  among the live siblings in the placement bucket, allocated under an advisory lock inside the
  create's own transaction, so it does not exist until the row does. The form writes it as the token
  `n` (`display-n`, `boardroom` for a mint that suppresses its first ordinal, `n` alone for a
  positional type), always with the sentence that makes it honest. A draft-preview verb that minted
  and rolled back was refused twice over: its answer is provisional, since another create can take the
  ordinal in between, and the rolled-back mint takes the lock real creates need, so previewing on
  every picker change would serialise the estate's creates behind a UI affordance. Re-rendering the
  label rule in TypeScript was refused outright as a second implementation of the engine slice 3 swept
  42 copies of, so the label is not previewed at all.

  The placement is shown as a **path** beside the field and never as a prefix inside it. The
  sub-issue asked for the ancestry as a read-only prefix on the name, which two slices had already made
  meaningless: names became scoped to placement, so a name no longer contains its ancestry, and
  printing it back into the field would restore exactly the redundancy the scoping removed.

  The three forms converge on one `CreateIdentity` section rather than a third copy, the two type
  registries' inheritance walks converge on one primitive with the stem as their third consumer, and a
  name is now optional on all three, required only where nothing will generate one (an unclassified
  system, a `location_type` with no name rule, a `component_type` chain with no stem), which the form
  names rather than just refusing. The browser tier grew the case that closes the epic: it reads the
  shape the console resolved in the browser, creates with the name left blank, and asserts the row
  landed with that stem and an ordinal the console could not have known. That runner also moved onto
  the dockerised, no-published-ports recipe the docs capture already used, since the old one could not
  start whenever another worktree's dev stack held 5432.

- **The create form locks the generated name and label until you override**
  ([#699](https://github.com/hyperscaleav/omniglass/issues/699), the tenth slice of
  [#657](https://github.com/hyperscaleav/omniglass/issues/657),
  [ADR-0104](/architecture/decisions/#adr-0104-a-create-form-shows-the-name-it-can-know-and-never-mints-one-to-preview-it)
  amended). Slice 8 showed the generated name as a hint line above two empty editable fields, which
  put the platform-owned path in the position of the fallback and the operator's in the position of
  the default, exactly backwards, and said nothing at all about the label. Both fields now open
  **locked**, each filled with the value the platform will actually use, each with its own Override.

  The ruling that made the label showable is that **a render is not a mint**, which ADR-0104 turned on
  without naming. Both of its refusals are about ALLOCATING: a minted ordinal is provisional because
  another create can take it before the commit, and a rolled-back mint takes the bucket's advisory
  lock, so previewing per picker change would serialise the estate's creates. Neither reaches an
  operation that allocates nothing. `POST /<collection>:renderLabel` resolves the rule through the
  same tiers, builds the same closed data map, executes it with the same one engine, and writes the
  token `n` where the ordinal would go. The TypeScript refusal is untouched and is what forces this
  shape: the label is shown because the Go engine rendered it, not because the browser learned to.
  The no-allocation claim is proven rather than asserted, by reading back every SQL statement the
  render issued (#650's counting instrument) and by a create five renders later still taking ordinal 1.

  It is gated by the entity's own `:create`, the permission the create it precedes needs. The
  PLACEMENT is a separate question, because the rendered string can carry a location's label and a
  system type's label, so those refs resolve within the caller's `location:read` and `system:read`
  scopes and a placement out of scope is refused rather than rendered; the test that holds it drives a
  principal scoped to one wing, not an owner. A location draft injects no scope, because a location's
  data map reads no other estate row.

  Each field has **three** states, and the third is not a loading one. Derived from the refusals in
  the code rather than from a list, they are: a `component_type` chain with no stem, a system with no
  `system_type`, a `system_type` chain with no stem (which the console's copy had folded into the
  previous one), a `location_type` with no name rule, and, on the label side, no rule resolving at any
  tier. That last is the DEFAULT state of every location create form in a fresh estate, since no
  location label rule ships at any tier, so the locked field shows the NAME (the read ladder's third
  rung) and says why, rather than sitting locked and empty.

  The wire contract is structural rather than remembered. A `Pen` clears its value when the lock
  closes, so "locked" and "posts nothing" are one state and a page's create body stays the `value ||
  undefined` it already was, and the draft body mirrors the create body field for field, including
  the omitted-means-generate name.

- **The epic's review pass, before it shipped** ([#657](https://github.com/hyperscaleav/omniglass/issues/657)).
  Four defects found reading the eight slices as one diff, each fixed with the test that failed
  first, and each of a kind the slice that introduced it could not have seen from inside itself.

  **A deleted system left every member component's label stale.** `system_member_system_id_fkey` is
  `ON DELETE CASCADE`, so deleting a system had always taken its memberships with it, inside the
  parent row's own `DELETE` where the gateway can hook nothing. Slice 5 derived the label write paths
  from the data map and caught every EXPLICIT mover of a primary membership; this is the one the
  database performs. A component in a boardroom under `[{{.SystemTypeLabel}}]` went on reading
  `[Boardroom]` after the boardroom was gone, and the epic's own invariant said so, which is the
  property it exists for. The memberships are now released explicitly one step ahead of the row's
  delete, so a delete is the same act `RemoveMember` performs and carries the same two consequences:
  the sole surviving membership is promoted to default (a divergence between the two doors into
  `system_member` that predated the epic, fixed here because the label the delete leaves behind
  depends on which membership answers afterwards), and the released components restamp in one
  recompute. The generic scoped delete grew a `beforeDelete` hook to hold it. The invariant fixture
  now drives a delete too: its claim that a write path nobody thought of fails it only ever reached
  the acts the fixture performs, and all fifteen of them were writes.

  **A `:move` that changed no bucket renamed a platform-named system.** The re-mint was gated on the
  row being platform-named alone, so a move that re-stated the location a system already sat at, or
  supplied neither field, re-entered the generator; with a lower ordinal freed by an earlier
  `:rename` that does not recompute to the same answer, it MOVES the name, under `system:move` with
  no rename asked for. It is the reclassify defect ADR-0101 already refused, wearing the other verb's
  clothes. The location arm shipped narrow one slice later and recorded the system arm as
  deliberately wide "because narrowing it would change an existing expected value"; the premise was
  false, since systems had only had generated names since this same unmerged branch. Narrowed, on the
  `nameScope` bucket rather than the two pointers, because a parent wins over a location. No test
  expectation moved. `MoveComponent` has the identical shape, is reachable the same way, and is
  genuinely pre-existing, so it is left for its own issue.

  **A platform-named location could not be reclassified, and neither the message nor the guide said
  so.** The refusal is correct and stays: re-minting is what a reclassify does to a name the platform
  owns, and a type with no rule leaves nothing to mint from. But `floor` is the only shipped type
  carrying a rule, so reclassifying a generated floor as a room, a building or a campus is the
  routine misclassification fix, and it is refused. The message said "name it yourself", advice with
  nowhere to take it, since the patch that reclassifies carries no name field; it now names the
  escape the way the system tier's twin does, and the operator guide documents the two-step fix where
  an operator meets it.

  **A label preview listed rows the apply would then refuse to touch.** The verb's two halves were
  wired to different narrowings: the apply selects on the caller's read scope AND their update scope,
  both injected into the one query, while the preview passed an all-scope action set that made the
  second predicate a constant. So an operator with estate-wide read and a narrow grant to update was
  shown a blast radius the apply then declined most of, with nothing on the wire explaining the gap.
  Both ADR-0100 and the recompute file's own argument for why a preview is an apply that rolls back
  already said a preview must list EXACTLY the rows the apply changes, so the scoping was the one
  place the code disagreed with the reason it exists. The preview now takes the same two scopes the
  apply does, and the e2e case that pinned the old answer (a preview of two rows against an apply of
  one) pins the new invariant instead: the two agree, row for row.

- **A rule can read a name as words, so the dictionary reaches something** (folded into
  [#698](https://github.com/hyperscaleav/omniglass/pull/698) after the rollup review,
  [ADR-0105](/architecture/decisions/#adr-0105-a-rule-reads-a-name-as-words-and-the-location-tier-ships-the-restatement-it-once-refused)).
  Ten slices built a label engine and an operator-owned acronym dictionary that no rule could reach
  from a NAME. `title` upper-cases each word and leaves the separator standing, so `{{title .Name}}`
  rendered `north-wing` as "North-Wing", and the closed FuncMap's other three ran the other way or
  ignored word boundaries entirely: "HQ West" out of `hq-west` was unwritable in any spelling, which
  is most of what the dictionary was for. `words` is the missing half, `slug`'s opposite number: a run
  of `-` or `_` becomes one space, a leading or trailing run is dropped rather than becoming an edge
  space, and everything else, including whitespace the fact already carried, is untouched. It is
  deliberately not `wordRe`'s "anything that is not a letter or a digit", since a catalog display
  name's parentheses and slashes are punctuation somebody chose.

  Adding a function to a closed grammar is a **three-place act** (the FuncMap, the AST allowlist,
  `FuncNames`), and two of the three is silent in both directions: a name in the FuncMap alone parses
  nowhere, and a name in the allowlist alone is just a hole in the allowlist. The published set is
  therefore walked by a test that parses each name in a real rule, so the three places are enforced
  rather than described.

  The **global location rule** then ships as `{{title (words .Name)}}`, which reverses half of the
  argument the seed file made against it. It said any rule at that tier is "either a constant or a
  restatement"; the constant half stands (labelling every room "Room" is worse than the name it would
  replace) and the restatement half was true only while a restatement could echo. This one re-cases
  the name and runs the operator's dictionary over it, so it produces a string the read ladder's last
  rung cannot. That rung is unchanged and still verbatim: a row with no stored label renders its name
  exactly. Shipping the rule restamps nothing by itself, because the blast radius is the whole estate
  and waits for the verb, so an existing install keeps its raw kebab locations until an operator runs
  `/locations:recomputeLabels`, and the operator guide says that rather than implying a release does
  it for them.

  The dev estate stopped masking it. #689 pinned a `display_name` on all thirteen locations because
  nothing would have rendered one; four of those pins now restate exactly what the rule renders, so
  they are released and the RENDERED values pinned in their place, which is what keeps an assertion
  behind a released pin. Nine survive with a reason each: `hq`, `west`, `east` and `airport` because a
  bearing or an abbreviation is not a place, `huddle`, `briefing` and `hall` because the room's noun
  is not in its name, and ADR-0103's two floors because a positional name is allocation order and
  releasing those two deletes that worked example from the estate. (Seven, after the floor reversal
  at the end of this log took the last two: a floor named for its designation renders that
  designation.) The media lab's name became
  `media-lab`, since every other location name was one word and nothing in `make dev` would otherwise
  show a separator becoming a space.

- **The pen becomes an inline action, and a locked field stops being a disabled one** (folded into
  [#698](https://github.com/hyperscaleav/omniglass/pull/698) after the rollup review,
  [ADR-0104](/architecture/decisions/#adr-0104-a-create-form-shows-the-name-it-can-know-and-never-mints-one-to-preview-it)
  amended). The lock #699 shipped was a text button on the field's label row, a second visual language
  for an idea the console already had: a square icon in the field's join, which is what a Variables row,
  a secret's reveal, and a setting's Restore to default have all been since they were built. It moved
  inside the field, and both actions now read as Settings' own does, an opening padlock to take the
  pen and the restore arrow to hand it back, with the same words on the tooltip.

  Dropping the text was not a restyle, because of what held the locked state: the field was
  **`disabled`**. A disabled input fires no click, so click-to-override is impossible on one, and it
  is out of the tab order, so the value the row is about to carry had no keyboard path at all. Losing
  the button's text without fixing that would have left the affordance discoverable only by hovering
  a field that cannot be focused. The fields are **`readonly`** instead: not editable, still
  focusable, still fires events.

  The locked LOOK then had to be drawn, and the reason is worth recording because the old code read
  as if it were already handled. daisyUI 5 ships **no `.input-disabled` class at all**, only
  `:disabled` and `[disabled]` selectors, so the `input-disabled` in the markup had been a no-op
  class since #699 and every bit of the locked appearance came from the attribute being removed.
  `.input-locked` in `app.css` carries it now, unlayered so it beats daisyUI's own `.input`, with the
  hover border and the pointer cursor as affordance on top.

  **Focus does not take the pen, and that is a deliberate departure** from the direction, which asked
  for click OR focus. A locked field is a tab stop, by the same decision that made it readonly, so
  focus-to-override would mean tabbing from the pickers to the Create button claimed both pens and
  blanked both fields on the way past, which is the state the locking exists to prevent. Clicking is
  a deliberate act; passing through is not. The click accelerator is also one-way: the way back
  discards what the operator typed, so it belongs on the button. A test walks the section's tab order
  and asserts all four controls are stops in both states, and another tabs through and asserts both
  fields are still locked on the platform's answer.

  The hints lost their instruction halves, since the icon and its tooltip carry the action now, and
  kept the facts: the placement the name has to be unique in, the rule that rendered the label, and
  the fact that is MISSING where nothing generates. Those three keep a "so name it yourself" tail,
  because that is the one state with no button in the field and the words are the only thing left
  carrying the next move.

- **A floor is named by the operator, not by the allocator** (folded into
  [#698](https://github.com/hyperscaleav/omniglass/pull/698) as an architect-directed reversal,
  [ADR-0103](/architecture/decisions/#adr-0103-a-positional-name-is-allocation-order-and-the-real-world-designation-is-a-label)
  amended, [ADR-0102](/architecture/decisions/#adr-0102-a-name-rule-is-a-declaration-a-type-opts-in-with-and-a-rule-change-renames-nothing)
  amended). ADR-0103 met the estate's floors and kept the divergence: the West Building's floor is
  the building's Level 2 and a positional generator calls it `1`, and the two fields were held to be
  answering two different questions. The architect rejected that for floors, and the argument that
  settles it is one the entry did not have. A floor's designation **is not an integer**. Buildings
  sign B2, LG, G, M, 1, 12A, P3, so an allocated ordinal is the wrong KIND of value rather than an
  imprecise one, which is what makes this obviously right instead of a matter of taste. It also
  dissolves the objection that looks hardest, since a negative floor is unspellable under the name
  rule but nobody signs a floor `-1`, they sign it `B1`, which is already a legal name.

  So `floor` loses its `name_rule` and joins campus, building and room as nominal, and the dev
  estate's two floors are named `level-2` and `level-1` for the designations they actually carry.
  The shipped location rule renders "Level 2" and "Level 1" from those names, so the two pins
  ADR-0103 called load-bearing are released and the estate's remaining hand-typed labels drop from
  nine to seven: name and label are one fact now rather than two that disagreed.

  **The cost is stated rather than hidden, which is most of the work.** No seeded location type
  carries a rule any more, so location name generation ships **dormant**: correct, tested, and
  demonstrated by nothing in a shipped estate. Seeding a fifth positional type to keep the demo
  alive was refused for the reason seeding a Level 1 was. Nine cases reached the generator through
  the SEEDED floor, and they now reach it through a positional type the tests create (a parking
  deck, whose number genuinely is an arbitrary disambiguator), because a feature losing its coverage
  when its last shipped user goes away is how one quietly stops working.

  The second cost is the one the seed model imposes and no amount of care avoids. `SeedLocationType`
  is insert-when-absent, so **removing the line un-ships nothing from an estate that already booted
  with it**: its `floor` still carries `{"stem": ""}` and still names floors `1`. That is ADR-0102's
  own consequence read backwards, and it composes badly with the other limit recorded there, since a
  rule cannot be CLEARED on the wire either (an omitted key and an explicit `null` both decode to a
  nil pointer). Such an estate reaches the new default only by a direct write, and a test now pins
  both halves rather than leaving them to be discovered: the re-seed leaves the rule standing, and a
  patch that omits `name_rule` leaves it standing too.

- **A create binds a placement it can read** ([#700](https://github.com/hyperscaleav/omniglass/issues/700),
  [ADR-0089](/architecture/decisions/#adr-0089-a-uuid-is-the-address-a-dotted-path-is-a-positional-lookup)
  amended). `CreateComponent` and `CreateSystem` resolved their `location` and `system` references
  existence-only, and so did the two `:move` verbs. That was defensible while a placement reference
  was only a pointer. It stopped being defensible when
  [ADR-0100](/architecture/decisions/#adr-0100-a-label-cascades-where-the-blast-radius-is-a-placement-and-waits-for-the-verb-where-it-is-the-estate)
  put placement into the label data map: the label these four writes stamp is rendered from the
  location's own label and the primary system's TYPE label, and the row comes straight back in the
  response, so naming a location you cannot read handed you its label. The
  [draft-render route](/architecture/decisions/#adr-0104-a-create-form-shows-the-name-it-can-know-and-never-mints-one-to-preview-it)
  built beside it is what made the gap visible: the console's preview was **stricter than the create
  it previews**, so an operator could be refused a draft for a placement and then create into it
  anyway.

  All four now resolve the placement through one seam, `resolvePlacementRef`, in the caller's
  `location:read` / `system:read` scope, which is the scope `:renderLabel` already injects for the
  same references. The refusal is the read path's **non-disclosing not-found** rather than the write
  path's 403: a status that separated "no such location" from "a location you may not see" would be
  the same disclosure one level up, and sharing one refusal is what keeps the preview and the create
  from disagreeing. The moves are in scope for the same reason the creates are, since a relocate
  restamps the label from the DESTINATION. The reclassify paths are not: `UpdateComponent` and
  `UpdateSystem` resolve only catalog rows (product, standard, system_type), which are not scoped
  trees, and touch no placement reference at all.

  Two consequences are stated rather than left to be discovered. A caller holding no grant on a tier
  now binds nothing on it, because it reads nothing on it: while the cross-tier scope expansion is
  unbuilt (#10), a component-scoped grant confers no `location:read`, so such a caller can no longer
  place a component at any location. That is the same sentence the read side already said, and the
  draft route already enforced. And the ambiguity redaction these binds carried (`withoutCandidates`)
  is now belt and braces rather than the load-bearing guard it was, since narrowing to the caller's
  read scope means a candidate list can only name rows that caller may read; making it useful again
  is [#697](https://github.com/hyperscaleav/omniglass/issues/697).

- **A re-mint follows the mint's inputs, not a field's presence**
  ([#696](https://github.com/hyperscaleav/omniglass/issues/696),
  [#691](https://github.com/hyperscaleav/omniglass/issues/691),
  [ADR-0101](/architecture/decisions/#adr-0101-the-first-of-its-stem-in-a-bucket-carries-no-ordinal-and-the-mint-that-says-so-is-the-one-allocation-tests)
  amended). The component tier catches up with the system tier on the two guards that decide whether
  a platform-owned name is minted again. `MoveComponent` re-minted whenever the row was
  platform-named, so a `:move` that re-stated the location the component already sat at moved its
  name; `UpdateComponent` re-minted whenever the `product` field was PRESENT in the patch, so a save
  that re-stated the product did the same. Neither is a harmless recompute, because allocation is
  lowest-free: an ordinal freed by an earlier `:rename` means the re-mint hands `display-2` the name
  `display-1`, under `component:move` or `component:update`, with no rename requested and possibly no
  `component:rename` grant held. The label follows the name (it reads the ordinal), so the silent
  rename silently relabelled too.

  The move guard is the system tier's, unchanged: the placement **bucket** compared as a `nameScope`
  value rather than as two pointers, because a parent wins over a location and a parented component
  that merely relocates has moved a pointer and not a bucket. The reclassify guard is deliberately
  NOT the system tier's. A system reads its stem from a `system_type` chain; a component reads it
  from a product that points at a `component_type` chain, one hop further, so two products under one
  type mint identical names and a product-id comparison would still move the name on a real
  reclassify. It compares the resolved **stem**, with `stemForProduct` lifted out of
  `generateNameForProduct` so a guard can ask what a name would be minted from without minting one.
  The matching residual one tier up (two `system_type` rows inheriting one stem) is
  [#706](https://github.com/hyperscaleav/omniglass/issues/706).

  Six tests pin both directions on both verbs, since a guard that suppresses too much is the same
  defect from the other side: the two no-op acts, the parented relocate a pointer comparison gets
  wrong, the same-stem reclassify a product-id comparison gets wrong, and the two real acts that must
  still re-mint with the label following. No existing expectation moved.

- **A shipped location type is platform-owned, and a name rule can be cleared**
  ([#703](https://github.com/hyperscaleav/omniglass/issues/703),
  [#692](https://github.com/hyperscaleav/omniglass/issues/692), ADR-0106). The two halves of one
  question: how does a shipped rule stop applying? A release withdraws it, and an operator clears
  their own.

  The seed's contract said authoritative upsert and `SeedLocationType` said
  `on conflict (name) do nothing`, which made a shipped default a one-way ratchet: adding one
  reached every estate, removing one reached only new installs. That was not an oversight. Shipped
  location types seeded `official: false`, so the row belonged to the operator and an authoritative
  re-seed would have stomped their edits. So the ownership model is the fix rather than the seed
  statement: `location_type` becomes the second adopter of the registry fork (ADR-0095), shipped rows
  seed official, an operator's edit stores their whole version of the mutable columns in
  `registry_shadow` under the row's own uuid, and `POST /location-types/{id}:restore` discards it.
  Every read resolves the shadow over the row, the placement check and the label and name generators
  included, so a fork reaches the surfaces it exists to change.

  The wire half is the mask (ADR-0091): `{"update_mask": ["name_rule"], "name_rule": null}` clears a
  rule, because an object has no empty value to overload the way a string has `""` and an explicit
  null is indistinguishable from an omitted key after decoding. It composes with the fork rather than
  arguing with it: clearing on a shipped type forks the row with an image carrying no rule, clearing
  on an operator's own type writes null, and an operator sees one behavior.

  The migration is one statement, and the interesting part is what it stopped doing. It first
  captured operator edits into `registry_shadow` ahead of the flip, telling an edit from a shipped
  value by the **audit trail**, since a row holding a value a previous release shipped and this one
  withdrew looks exactly like an edit and preserving it would defeat the withdrawal. The architect
  ruled that premise away: no release is cut and no operator data exists, so there is no upgraded
  estate and nothing to preserve, and a review had already found the discriminator wrong in two ways
  that both lose data silently with its `create` leg exercised by nothing. So the backfill is
  `set official = true` on the four shipped names, its test asserts it writes no shadow at all, and
  ADR-0106 records that the removal rides on the premise: the first real release has estates, and
  owes them a migration that reasons about their rows. Still proven against the old shape, standing
  the database one migration back, writing the rows an installed estate holds, migrating forward with
  the real file, and driving the down leg as a round trip.

  One capability had to be defended rather than shipped: flipping the rows to official would have
  activated a guard dormant on this registry and silently taken away the contract editor for the four
  types an estate actually uses. A contract line is a row in its own table, nothing seeds one, so
  those writes no longer consult the official flag. Two expectations inverted deliberately, the seed
  test's "official location_types = 0" and the wire test's twin; the console's origin column now
  reads three ways and its blade offers **Restore shipped**.

- **A create form shows the real ordinal, and a conflict is refused rather than renumbered.** A create
  form showed the token `n` where a generated name's ordinal would go (`display-n`) and the row then
  landed `display-1`, so the operator was never shown the name they actually got, which is the promise
  the whole locked-field design rests on. ADR-0104 had refused a preview because previewing meant
  MINTING, and a mint takes the bucket advisory lock real creates need: previewing per picker change
  would have serialised the estate's creates behind a UI affordance.

  Reading the lowest free ordinal is not minting it. It is the computation the allocator already runs
  over the sibling names in the placement bucket, with no lock, no write transaction and no
  allocation, so `:renderLabel` now answers with the whole drafted **name** beside the label and the
  form shows `display-3`. That the read allocates nothing is held by the counting instrument reading
  back every statement the render issued, and by a create five renders later still taking ordinal 1.

  The answer is provisional by nature, and that is answered rather than hidden: the form posts the
  number back as the create's `expected_ordinal`, the create allocates under its own lock exactly as
  before, and one that would land a different name is refused with a 409 naming the number that moved.
  The form re-reads and shows the new name. It posts a number and never a name, because a locked field
  posting a name would claim the pen and invert the feature; the API refuses the pair (422) where an
  operator supplied the name, since nothing is allocated there for the expectation to be about.
  (Corrected in the review below: the field is `expected_name` and carries the drafted name, which is
  the claim the locked field is actually making. It is still not the name field.)

  A create form is the one caller that cannot treat a refusal as "show it and stop", so the two
  refusals it has to act on carry a machine-readable location rather than only a sentence: the
  conflict on `body.expected_ordinal` (`body.expected_name` after that review) and "the platform will
  not name this row" on `body.name`. That is the RFC 9457 `errors` array the error model already
  publishes.

  Returning the name also ended a duplication: the console walked the type chain in TypeScript to
  resolve a stem, and the gateway walked it in Go, held together only by one browser end-to-end test
  (#695). The browser no longer knows what a mint looks like, and the three placement pickers feed the
  draft body because the ordinal is read from a BUCKET, which a draft that ignored the parent would
  have got wrong.

- **An ambiguous placement names the rows it matched.** Every building's first floor may be named `1`,
  so a create or a move that binds a location by bare name matches one row per building and answered
  `"1" is ambiguous for location (matches )`: the operator was told their input was ambiguous and
  handed nothing to disambiguate with. The list was empty on purpose, and the reason had expired. It
  was stripped because a candidate could be a row the caller holds no grant to read, and disclosing
  that such a row exists, even only as a uuid in a 409, is the leak the non-disclosing not-found
  exists to prevent; the slice before this one narrowed the bind to the caller's own read scope on the
  referenced tier, and the reference primitive filters the match set through the scope tree BEFORE it
  judges ambiguity, so every candidate is now a row that caller already may read.

  Only that one seam changed. The redaction does two jobs, and each of the other call sites still
  needs it, so it kept them: the three availability advisories resolve scope-blind by design (they
  answer about the placement bucket asked about, not the caller's grant), and the component end of a
  membership write, a role write and a tag resolve holds only a scope resolved for the other tier,
  which can never narrow a component lookup. The redaction's second job, folding a structural
  path miss down to the bare sentinel so a create's missing location stays the 422 it is rather than
  becoming a blanket 404, is now its own function and is what the placement bind still calls.

  The listed candidates are uuids, and a uuid resolves this same reference, so the answer is
  actionable rather than only descriptive. So is the dotted address the issue assumed did not exist
  here: the body reference has parsed one since addresses landed, which the slice pins with a create
  that binds `hq.wing-a.west.1` where the bare `1` is refused.

- **A create that writes a membership costs what the membership route costs.** `POST /components`
  accepts a `system` and inserts that component's primary membership from it, the same row the
  membership route writes under `system:update`, while the create asked for no system permission at
  all. The gate on the membership route was therefore decorative: the create was the cheaper way
  around it. The create now requires `system:update` when the reference is present, and resolves it in
  that scope rather than in the read scope the location beside it uses, because what decides the
  action a placement reference resolves for is what the write DOES with it.

  The consequence was accepted rather than worked around: `operator` holds `component:create` and no
  system permission of any kind, so an operator can no longer create a component into a system. That
  is the line the seeded roles already draw (an operator maintains components, a deploy tech builds
  out systems and their membership), and granting `operator` the permission would have handed it
  general system editing to buy one binding.

  A narrowing discovered as a 403 after a form is filled in is the outcome the do-not-offer-what-the-
  platform-refuses rule exists to prevent, so the console does not offer the system picker to a
  principal that cannot use it: the slot keeps its place in the placement grid and explains itself,
  naming the permission to ask for. The API's refusal names it too, which is what the CLI prints and
  what its generated help now says.

  Two paths write one row, so the spec has to say so. The second permission is conditional on the
  request rather than on a tier, and middleware cannot see a body, so the check runs in the handler
  and the permission is published as `x-omniglass-conditional-permission` beside the route's primary
  stamp, the same shape a platform-tier write already used. It joins the route-derived permission
  universe, so the roles view and the docs lint both see it without being told separately.

- **The membership gate is two layers, and a refusal names the one that is missing.** The review of
  the slice above found both halves of "the narrowing is met before the form is filled in" untrue for
  a shipped shape. The console gate read the `system:update` PERMISSION and nothing else, so a
  principal holding it over an empty scope was offered the picker, filled the form in and was refused
  on submit. And the bind resolved in the `system:update` scope alone, so a system outside it came
  back as the non-disclosing not-found and the route answered "system not found" for a row the same
  caller could `GET`.

  The principal is not exotic. `applicableKinds("system")` is `{"system"}` alone and the cross-tier
  expansion is unbuilt (#10), so a location-scoped `deploy` grant fills no system-tier scope at all,
  and devseed ships exactly that as `tech-east`. Driven end to end, that grant can do NO system work:
  create, update, rename, move and all three membership writes refuse it, and without a second grant
  it cannot even list a system. So the previous slice did not break a working role, it extended a
  pre-existing gap to one more path, which is why the fix here is an honest refusal and a console
  that does not offer what the server will decline, rather than the cross-tier expansion.

  The bind now takes `system:read` beside `system:update`. Update still decides whether it happens;
  read decides what the refusal may say. Out of the read scope is the same non-disclosing 422 as
  before, so nothing new is disclosed; inside it, a **403 names the scope**, which discloses nothing
  either, since that caller can read the row. The console gate now also requires a system carrying
  the scope-aware `update` action (the server's own per-row answer, from the same per-action scope
  the gateway enforces), offers only those systems, and says which of the two layers is missing.

- **A draft does not preview a bucket its create refuses, and the precondition binds the name.** Two
  more findings against the create form's identity render.

  The draft resolved an empty parent to "the parentless bucket" with no gate, and every create
  refuses that bucket without an all-scoped grant. The answer is not inert: it carries the lowest
  free ordinal, read from the bucket's sibling NAMES, so it reports which of that bucket's names are
  taken, and the stem asked about is the caller's to choose, since writing one into a forked type's
  name rule needs only `location_type:create`. Driven, the root bucket answered `secret-region-2` to
  a principal whose create there is a 403. One seam (`draftParentID`) now applies the create's own
  gate for all three tiers.

  The precondition bound the ordinal, and the ordinal is not the claim a locked field makes. A name
  is the stem, the suppression rule and the number together, so `PATCH /component-types/display
  {"stem":"monitor"}` under an open form left the number untouched and the row landed `monitor-1`
  where the field showed `display-1`, precondition met. `expected_name` replaces `expected_ordinal`
  on all three creates and carries the whole claim by construction, so no future input to a mint has
  to be remembered here. It is still a precondition and not the name field: the create leaves `name`
  empty, the row is still `name_generated`, and posting it beside a supplied name is the same 422.
  The 409's machine-readable detail moves to `body.expected_name` and carries the name the create
  would have produced, which is what the form shows next.

- **An inherited component-type fact rides as omitted, never as an empty string.** The component-type
  edit blade posted `stem`, `abbrev` and `icon` as raw signals seeded with `?? ""`, so a node that
  inherits a fact sent `""`. Those columns are nullable and the server's walk treats only NULL as
  inherit, while the patch coalesces, so the empty string wrote a real value that stopped the walk for
  that node and every descendant under it: silent, permanent, and on the facts the generated name and
  the abbrev-compacted render read. The console hid it in both directions, since the wire omits an
  empty icon and the client-side resolver treats `""` as falsy and keeps walking, so the page still
  drew the inherited glyph while the server no longer resolved one. `stem` also carries a minLength on
  the patch body, so a custom child that legitimately has none could not be edited at all: the save
  was a 422 before the handler ran. This is the original of the defect the system-type copy fixed
  alongside it, and it takes the same shape (empty means omitted, plus a guard test asserting no `""`
  ever rides the body). Clearing a fact back to inherit stays inexpressible from the console; the
  instrument for it is the three-state string sentinel already live on `label_rule` in the same
  handler, not the write mask, which stays scoped to nullable object fields.

- **The recompute's lock order is one stated fact, and it is the id.** `locationsOver` ordered its
  result by `name` while `recomputeChain`'s comment said the order was by id and `lockHealthOwner`'s
  said it was by name: three statements, two of them wrong, and harmless only while a location name
  was unique estate-wide. Scoping name uniqueness to placement retired that guarantee, so two rooms
  under different buildings can both be `415a` and the comparison ties. A tie is not an order: it
  hands the visit order to the plan, and the plan reads its input, so the two production trigger
  shapes really did disagree. A location move (both rooms named outright) resolved the pair
  newest-first and a system move (one room reached through the system placed in it, the other named
  as the one it left) resolved it oldest-first, which is exactly the precondition the per-owner
  advisory locks assume away. The query now orders by id, matching what the two comments claim and
  what `refSet.sorted()` already did for the component and system tiers. The guard is the ordering
  itself, asserted two ways and deterministic in both; the concurrency test the issue asked for is
  kept as a liveness check and labelled as one, because driven against the unfixed ordering it
  survived over four thousand paired rounds without deadlocking and odds are not a guard.

- **Settlement reads one clock, and a zero window stops arguing with it.** `Settle` compared a
  sample's `ts`, written by Postgres, against a `now` supplied by the Go process, so the verdict was a
  function of the skew between two hosts. At a zero settle window the comparison reduces to "is this
  sample stamped in the future", with no margin at all to absorb the difference, which is how the
  setting whose whole meaning is "do not wait" became the one that could wait forever: a command that
  genuinely failed reported `pending` and never settled. It showed up first as an intermittent
  end-to-end failure and was reproduced on a branch containing no Go at all. Both settle paths now read
  `now` from the database in their own transaction, which at issue is the very timestamp the intended
  row was stamped with, and a zero window is terminal before any arithmetic runs. `Settle` stays pure
  and still takes `now`; what changed is who supplies it, at the cost of one round trip on each of the
  two paths. The alternative, stamping samples from Go, was refused as the larger ripple, and a
  tolerance was refused as the move that quiets a test without changing the behavior
  ([ADR-0108](/architecture/decisions/#adr-0108-settlement-reads-one-clock-and-a-zero-window-is-a-statement-of-intent)).

- **Two boardrooms in one room read differently.** A divisible boardroom is two `board` systems in
  one room, and the shipped system rule read the type alone, so both rendered "Boardroom": the
  platform could tell them apart (`boardroom` and `boardroom-2`, ADR-0101's suppression) and the
  operator reading the console could not. The rule now reads the ordinal under the component's own
  `{{if .Ordinal}}`, and the estate's two halves are the demonstration rather than two pinned labels
  spelling A and B over the top of it. What the issue and the ruling both described as one change was
  two, and the second is where the argument sits: the rule alone renders "Boardroom 1" for the only
  boardroom in a room, because `{{if}}` is false for an EMPTY string and a suppressed first name
  still owns the stored ordinal 1. So the data map's `Ordinal` became the number the row's NAME
  shows, asked of the mint rather than read off the name, which makes a label and a name unable to
  disagree about how many of a thing there are and costs the ability to author "Boardroom 1" for a
  system called `boardroom`, a string this arc already calls the defect it was filed about. Every act
  that moves a system's ordinal was re-derived and the set grew by none, because each of them already
  restamps unconditionally; the estate-wide invariant now runs a system rule that reads the ordinal,
  where the one before it could not have seen a hole in any of this. A shipped rule change restamps
  nothing, so an existing estate keeps both halves reading "Boardroom" until an operator runs
  `/systems:recomputeLabels`, which the seed, the architecture page and the operator-facing rule text
  each say where they are met
  ([ADR-0101](/architecture/decisions/#adr-0101-the-first-of-its-stem-in-a-bucket-carries-no-ordinal-and-the-mint-that-says-so-is-the-one-allocation-tests) amended).

- **The Name column stops being the first thing a narrow screen drops.** A list table is
  `table-layout: fixed` and every column but Name declares a width, so Name takes what is left. That
  is what lets the identifier grow into a wide screen, and it also made Name the first column to give
  space up on a narrow one, to the point of vanishing: measured against the dev estate at a 1280
  viewport, where the list card offers 973px, the Components table's Name column was **0px** wide
  while Tags kept all 340 of its pixels, and Systems was the same at 0px against 960px of declared
  columns. Locations, declaring 650px, had 173px left over and looked fine, which is the whole of why
  one page was reported and three were affected: identical markup, three outcomes, decided by the
  widths each page happens to declare. The remedy is one floor in the shared shell rather than
  numbers tuned per page: the table asks for a `min-width` of everything declared plus a Name
  minimum, so the browser gives Name that much and the card (already `overflow-x-auto`) scrolls
  sideways when even that does not fit. Wide screens are untouched, since `width: 100%` beats a
  smaller min-width and Name still absorbs the surplus. Held by a page test on each of the three
  pages (Name declares no width, and the table's floor leaves it the minimum) and by a browser test
  that MEASURES the rendered column at 1280 and 1366, which is the tier the defect lives in: a
  column measuring zero pixels sat on main behind a green suite because nothing below a real browser
  does layout.

- **The label pen leaves the list and lands on the field it owns.** A display name the platform
  rendered from a label rule wore a full-text `Generated` chip in the identity cell, read by 18
  flat-list pages and every tree. It charged the Name column the width of the word on every
  platform-labelled row, on the column a floor had just had to be put under, and it stated an
  ownership fact where an operator could do nothing about it. The chip is gone from both list
  renderers, and the fact is the **lock** on the display-name field of the edit blade: the create
  form's own affordance, extracted so the two surfaces share one button, one pair of icons and one
  set of words rather than growing a second vocabulary for one idea. The NAME's chip stays, beside
  the name on the component blade, which is the rule both now follow: a pen states itself beside the
  field it owns, where the operator can act on it. The whole-estate question the chip half-answered,
  which rows a rule edit would rewrite, is answered whole by `<entity> previewLabels`. Doing it
  closed a defect nobody had filed: every blade seeded its label input from the stored value and
  posted `display() || undefined`, so opening the pencil on a platform-labelled row and saving a tag
  or a type posted the platform's own rendering back as an override and took the pen silently. The
  field posts the pen's own value now, which is the empty string while it is locked and is how the
  API says "still the platform's", so the same expression covers the no-op and the first way back the
  console has ever had
  ([ADR-0104](/architecture/decisions/#adr-0104-a-create-form-shows-the-name-it-can-know-and-never-mints-one-to-preview-it) amended).

- **The Name column's floor is re-measured now that nothing sits beside the label.** The floor was
  260px because the identity cell had to fit a label AND the label pen's `Generated` chip; the chip
  left in the same slice, so the number it was tuned for is stale. Re-driven against the dev estate
  at a 1280 viewport, measuring each row's name cell twice in one run (once with the chip's exact
  markup injected back into the clone), the chip cost a **uniform 69px** on all 20 rows of the three
  pages, and the estate's widest cell fell from 256px to 187px. The floor is therefore the old one
  minus the chip, **191px**, rather than the "about 200" it was estimated at: the labels did not
  change, only the thing beside them, so the floor should carry exactly what it carried before. The
  method is written out beside the constant so the next move can be checked, and it was validated
  before the number moved by reproducing this comment's own previous measurement ("Boardroom 2" wants
  203px with its chip, recorded as 200). One page's sideways scroll actually goes away: Components at
  a 1680 viewport now asks 1231px of a 1254px card where it asked 1300px. Systems still scrolls there
  (1301px) and Locations still scrolls at 1280 (991px of 974px), and nothing truncates on any page at
  1280, 1366 or 1680.

- **A preview is refused wherever the create it previews is refused.** `POST /components:renderLabel`
  resolved its `system` reference in `system:read` while the create beside it had moved to
  `system:update`, so a caller holding the read floor and no membership authority was served a
  drafted label for a create the platform then refused, and the label it was served carried that
  system's own type name. Nothing hit it in practice, because the console does not offer the picker
  to a principal that cannot use it, which is the agreement being kept by the caller's good behaviour
  rather than by the platform. The draft now resolves that reference through the create's own
  resolver, in `system:read` and `system:update`, and carries the create's conditional
  `system:update` permission, so both halves of the gate rehearse and both routes answer the same
  sentinel: 403 naming the authority for a readable system, the non-disclosing 422 for one the caller
  cannot see. The LOCATION reference on the same two routes was checked in the same pass and is
  already aligned at `location:read`, deliberately, because a location is read and rendered into the
  label rather than written
  ([ADR-0107](/architecture/decisions/#adr-0107-a-create-that-writes-a-membership-costs-what-the-membership-route-costs),
  [ADR-0104](/architecture/decisions/#adr-0104-a-create-form-shows-the-name-it-can-know-and-never-mints-one-to-preview-it)).

- **A settle window is a duration, and the platform now says so.** `settle_window_seconds` had a
  default of 0 in four places and a floor in none, so a negative one was accepted by the column, the
  API body, the console and the seed loader alike. It is not a shorter wait: `Settle` tests
  `windowSeconds > 0`, so -5 behaves exactly as 0 does, which made it a value an operator could set,
  read back on the row, and never see honoured on any command of that type. The schema now floors the
  field at 0, so the generated client and CLI carry the floor too, and the gateway refuses one on
  create, on update and on the boot-seed upsert, the last naming the row the way its target refusals
  already do. Zero stays legal and unchanged: it is the documented way to say "settle immediately"
  ([ADR-0108](/architecture/decisions/#adr-0108-settlement-reads-one-clock-and-a-zero-window-is-a-statement-of-intent)),
  and it is what the shipped `reboot` carries. In the same pass, the settle-check's comment claiming
  its `now()` is strictly later than the intended row's `ts` was corrected: READ COMMITTED does not
  provide that ordering, and while no verdict is wrong today (a negative delta is `pending`, and the
  zero case reads no timestamp at all), an invariant the isolation level does not give is one a later
  change can lean on.

- **The deploy role stops claiming a reach a location grant has never had.** Its operator-facing
  description, rendered in the Roles view and in the grant builder's tooltips, said that granted at a
  location it "builds out and edits everything inside a subtree (add rooms, systems, and
  components)". It does one of those three. A grant contributes to a resolved scope only when its
  scope kind can CONTAIN the resource, and each tree tier is contained by its own kind alone, so a
  location-kind grant fills no system-tier and no component-tier scope whatever actions the role
  carries: the shipped `tech-east` principal cannot create a system in its own building, and cannot
  read a component in it either. The gap was silent and expensive in exactly the way a stale sentence
  is: an admin granted the role expecting one thing, the console offered the surfaces, and the server
  refused. The description now says what the grant does, and two tests hold it there: a pure one over
  the embedded `roles.yaml` that computes the whole reach matrix through the same resolver the
  gateway uses (so a cross-tier rule landing shows up as a failing expectation rather than as
  prose going stale), and an end-to-end one that drives the component tier the way the system tier
  was already driven. The capability itself, a scope that spans tiers, is
  [#10](https://github.com/hyperscaleav/omniglass/issues/10) and unbuilt; this slice is the claim.

- **A series tiebreak is checked against the database rather than against a reading of the code.** A
  single observed flake was filed as the series resolver breaking a `ts` tie on a random uuid, with
  the mechanism read off the resolver rather than off the failure. The premise does not hold, and the
  tiebreak the issue asked for is the one already there: `property.id` is a bigint identity column,
  so `order by ts desc, id desc` breaks a tie on insertion order, and the test the flake came from
  cannot tie at all, since every declared write is its own transaction and `ts` defaults to
  `transaction_timestamp`. No fix shipped for a mechanism that does not exist. What shipped is the
  proof: a test that reads the column's shape out of the live catalog, so converting it to a random
  uuid fails loudly with the reasoning named, and that drives a genuine tie the public API cannot
  produce, resolving it to the later insert twenty times over. The ordering's one real dependency is
  now stated where it is relied on: `ts` leads because the observed lane accepts a caller-supplied
  timestamp, so a clock stepping backwards between two writes to one series resolves to the
  earlier-written row, which no tiebreak can reach. Sixty-five repeats of the original test, forty of
  them under concurrent package load, did not reproduce the failure.

- **The console stops answering a question only the operator can answer.** A command type naming a
  target arm is asking to be settled against a reported value, so its settle window decides when a
  difference becomes a verdict. The create form seeded that field with `0` and folded it with
  `Number(settle()) || 0`, so an untouched field, a typo and a deliberate zero all reached the
  server as the same explicit `0`: a settleable type judged at the instant of issue, which nobody
  chose and no refusal announced. The value was never the defect. Zero beside a target is a
  statement of intent, the documented way to say "judge it now"
  ([ADR-0108](/architecture/decisions/#adr-0108-settlement-reads-one-clock-and-a-zero-window-is-a-statement-of-intent)),
  and refusing the combination at the gateway would have reversed a decision accepted the day
  before and broken its own clock regression test, whose observable does not exist at a positive
  window. The default was the defect. The field now starts blank on the create form and the window
  is read through one rule both write surfaces share: a settleable type must state its window, and
  the create and the blade's Save are refused until it does; a fire-and-forget type sends no window
  at all and takes the column's own 0, which is `reboot`'s shape unchanged. Unstated survives as
  far as the body, where it is a field that is simply not sent, so it can no longer be confused with
  a chosen zero. What a stated zero does is now legible where it is typed, next to the field rather
  than only in the API reference, and a typed negative is refused there too: `min="0"` had been on
  that input since the command pillar landed and refuses nothing, because both submit paths are
  JavaScript (the drawer's action bar and the blade's Save sit outside any form), so native
  constraint validation never runs on the path an operator uses. The filed issue's own account of
  the symptom was wrong and is corrected with it: a zero-window settleable command is `settled` when
  the observed value already matches, `failed` when it differs, and `timed-out` only when nothing
  has been observed, not `timed-out` unconditionally.

- **The admin directory stops reading at 1+3N**
  ([#671](https://github.com/hyperscaleav/omniglass/issues/671)). `ListPrincipals` drained its base
  query and then called `loadPrincipal` once per row, three statements each (the kind profile, the
  effective grants, the group memberships). Measured on the counting instrument that found it, the
  admin directory cost seven statements for two principals and sixty-seven for twenty-two, and it is
  the one directory that grows with the organisation rather than the estate, with no pagination to
  hide behind. It now costs **four, whatever the page size**: the base query, then one union over the
  three profile tables, one over the effective grants, one over the group memberships, assembled in
  memory by principal id. The shape is [#648](https://github.com/hyperscaleav/omniglass/issues/648)'s,
  reads keyed by id with the single-row function kept as the oracle, and `loadPrincipal` is unchanged
  and still the only path `GetPrincipal` takes.

  The projection did not narrow, and the console is why: the directory renders a grant COUNT and a
  badge per group a principal belongs to, so the list needs the same effective grants and memberships
  the detail blade does. A lighter row would have been the smaller fix and would have emptied two
  columns. Nothing about what the directory returns changed, which is what let the whole principal
  suite stay green with no expectation edited.

  The kind profile is a **union** rather than a query per kind present in the page. A branch per kind
  would have been flat in page size too, but its cost would have depended on the MIX, a directory of
  humans, service accounts and nodes costing two statements more than one holding only humans, and a
  count that varies with the fixture's composition is a count no assertion can state as a number. The
  cost fixture holds a node beside its humans to keep that honest rather than assumed, and grows the
  grants and the group memberships with the page as well as the principals, because a page whose rows
  carry no grants leaves the grant assembly flat by accident.

  Two gates, both of which the batch had to earn. `TestBatchPrincipalLoadMatchesSingle` holds the batch
  to the single-row loader field for field over a fixture carrying all three kinds, direct grants,
  inherited grants, both, neither, and a group with no display name; equality, not improvement, since a
  batch that answers differently from the loader every other read path uses is a second implementation
  rather than a fix. And the batch refuses what the oracle refuses: a principal of a profiled kind with
  no profile row is an error on both paths, not a directory row silently missing its username.
  `TestListPrincipalsCostIsPerRowToday`, which pinned the defect at its measured 1+3N and said in its
  own failure message that the fix was to delete it, is deleted; `TestListPrincipalsCostIsFlatInPageSize`
  takes its place beside the other nine.

- **The alarm write path gets the instrument that can see it, and it is not flat**
  ([#674](https://github.com/hyperscaleav/omniglass/issues/674)). The recompute behind every alarm the
  platform raises was the one hot path with a two-digit statement count and nothing measuring it. A
  wall-clock benchmark of it was written during [#651](https://github.com/hyperscaleav/omniglass/issues/651)
  and deliberately dropped: against the measured 262 us round-trip floor it is about three-quarters
  transport, so doubling every query plan inside it would move the number by ~25%, barely above its own
  spread. Counting is the instrument that does not care that transport dominates.

  The issue named two candidate growth dimensions, the number of alarms and the number of roles the
  component fills, and asked which one the code actually varies over, because a fixture that holds the
  growing dimension constant is flat by accident and this repo has made that mistake twice. Measured
  over seven fixtures, two of them held back as out-of-sample predictions, **one closed form fits all
  seven and predicted the last two before they were run**:

  ```
  raise = clear = 12 + 5*S + 4*L
  ```

  S is the number of slots the component fills, L the number of distinct locations above the systems
  holding them. So the first candidate is **false** and the second is **true**. The count is flat in
  the number of alarms, because a component's open alarms are folded by one `array_agg`; it grows at
  five statements per staffed system (an advisory lock, a role resolution, and a recorded verdict that
  re-takes the lock) and four per ancestor location. The 58 the issue reports is this shape's minimum,
  one system three locations, counted as the raise and the clear together.

  Both facts ship as assertions, and the shape of each follows what is true rather than what would look
  tidy. The flatness in alarm count is asserted with an explicit guard that the two measurements were
  taken over genuinely different open-alarm sets, since a fixture that quietly deduplicated its alarms
  would report a beautifully flat number while varying nothing. The growth is **pinned as an equation**,
  the way [#671](https://github.com/hyperscaleav/omniglass/issues/671)'s defect was pinned before it was
  fixed, so a change is reported as which term moved; a ceiling generous enough to look comfortable
  would have hidden the slope underneath it. Reducing the count is explicitly out of scope: a reduction
  with no measurement in front of it cannot be shown to have worked, and this is that measurement.

- **The systems list stops fetching health once per row**
  ([#653](https://github.com/hyperscaleav/omniglass/issues/653)). Every row of the systems list
  rendered a health badge that owned its own query, so a page of N systems fired **N HTTP requests on
  first paint**, and each one resolved every role the system needs, its occupants, their alarms and
  thirty days of recorded transitions in order to render one word and a colour. `staleTime` softened
  the refetch and did nothing for the first load, which is the one an operator waits on. Measured on a
  twelve-system page: **twelve requests before, one after.**

  The badge needed no change. It has always preferred a verdict the caller hands it and only fetches
  when it has none, and the fix is at the call site: the page reads verdicts once and passes them
  through that prop. What the call site does NOT do is also load-bearing. It passes the verdict and
  **not** the system id, because the id is what makes the badge fetch, and a cell that passed one while
  the bulk read was still in flight would put the per-row request back precisely where it was removed
  from. The column stays quiet until the map lands, which is the behaviour it already had.

  The read the issue named, `GET /views/estate` from
  [#632](https://github.com/hyperscaleav/omniglass/issues/632), is not on this branch's ancestry, so the
  other option the issue offers is the one taken: a narrower bulk read of exactly what the column
  renders. `GET /systems:health` answers one verdict per system in the caller's read scope, in **one
  statement whatever the estate size**, built on the `distinct on` pass over the property series that
  `locationVerdict` already ships rather than a correlated latest-row subquery per system. Its scope is
  `ListSystems`' scope by construction (the same `scopedListSQL` over the same table with the same
  binds), so a caller gets a verdict for exactly the rows it gets, and an empty scope is an empty list
  rather than a refusal.

  Two things are asserted rather than assumed, because both are places this could quietly go wrong. The
  bulk read computes nothing: it reads the recorded series and reports a system with no recorded row as
  healthy, so it is held to `SystemHealth`'s own live-computed verdict over a fixture carrying all three
  verdicts **and** a system nothing has ever recomputed, which is the row that default exists for. And
  the health column's freshness now depends on a second cache key, so the role and member writes that
  already invalidated the per-system key invalidate the bulk one alongside it; without that the column
  would go stale silently, which is exactly the regression #627's review round 3 caught in the same
  place.

  The visible UI is unchanged, deliberately: the same badges in the same column. The measurement is the
  request count, asserted in the page test against a twelve-row page loaded cold, with the per-row
  endpoint still served by the fixture so that the old implementation fails on the NUMBER rather than by
  rendering nothing.

- **The health reads reach an index, and a perf guard may ask the planner what a query can reach**
  ([#725](https://github.com/hyperscaleav/omniglass/issues/725),
  [ADR-0094](/architecture/decisions/) as amended). The bulk verdict read shipped in
  [#653](https://github.com/hyperscaleav/omniglass/issues/653) made a systems list cost one statement
  instead of one per row, and the win was entirely in round trips: `property` carried no index the read
  could use, so one scan of the whole series replaced N health resolutions. This slice is the other
  half, the work done inside that statement, and it starts by settling what a guard on it is allowed to
  ask.

  The issue framed it as a standoff between a house rule (a guard asks the planner, not the catalog)
  and an accepted ADR (no `EXPLAIN` assertions), and there is no standoff: ADR-0094 **deferred** them
  and named its own revisit conditions, neither of which is met here. What it deferred was asking the
  planner what it **prefers**, and it was right to. On a small fixture Postgres correctly chooses a
  sequential scan even over a perfect index, so an assertion about the chosen plan pins the fixture's
  size. Asking whether an index is **reachable at all** is a different question, and the difference was
  measured rather than argued: planned with `enable_seqscan = off`, the health reads' scan of
  `property` was the same index scan carrying the same index condition on an empty database, on a
  45-row fixture with no statistics, and on that fixture analyzed, while the joins around it moved
  between hash and merge and a sort came and went. So the deferral of preference assertions stands
  untouched and a **usability** assertion is carved out, as `internal/storage/storagetest/accesspath`:
  one relation's access path, index name **and** index condition, gating in `make test` beside
  round-trip counting.

  The condition half is not decoration. Two of the four mutations run against this guard leave the
  index NAMED in the plan and empty out its condition (coercing the filtered column, dropping the
  leading column from the filter), so an assertion that greps for the index name passes while the read
  walks the whole index and filters afterwards. A third, giving the partial index a predicate the query
  cannot prove, leaves `pg_indexes` reporting the index present and the read scanning the table, which
  is the failure this instrument exists for and the reason a catalog test would have proved nothing.
  The guard explains the statement the gateway really issued, captured off the pool tracer
  (`querycount.Counter.Calls` now records arguments beside SQL) rather than copied into the test.

  Then the index, and it earned its place on measurement, at 1,521,600 property rows (334 MB, 1,500
  systems, 7,500 components, 18,000 recorded health rows): the bulk read went from **51 ms to 10 ms**,
  the location rollup from **45 ms to 0.7 ms**, and the transition probe every recompute pays from
  **6.1 ms to 0.06 ms**. `property_system_owner_idx` leads with `property_type_id` because both reads
  fix it by equality, which leaves the rest of the range ordered by `(system_id, id desc)`, exactly
  their `ORDER BY`, so no sort is needed; the reverse order was built and measured slower once systems
  carry properties other than health. It is partial on `system_id is not null`, which is what makes it
  free on the telemetry lane: 150,000 component-owned inserts cost 2237 ms with it and 2237 ms without.
  A covering `include (value)` variant measured faster still (8.4 ms) and was rejected rather than
  shipped, because an index tuple is not TOASTed, so a property value over about 2.7 KB would fail the
  INSERT rather than the read. A concurrent build was rejected too, and the reasoning is in the
  migration: it cannot run in dbmate's transaction, and a failed one leaves an invalid index that the
  retry's `if not exists` silently skips, which is the same silently-unusable-index class this slice
  exists to close.

- **The rule language's function set stops being typed four times**
  ([#701](https://github.com/hyperscaleav/omniglass/issues/701)). Adding `words` in
  [#657](https://github.com/hyperscaleav/omniglass/issues/657) meant editing the `template.FuncMap`,
  `FuncNames()`, the AST allowlist and the prose that teaches the language, and forgetting any one of
  them failed differently: a name in the FuncMap alone parses nowhere, a name in the allowlist alone is
  just a hole, a name in `FuncNames` alone is published and unreachable, and a name missing from the
  docs simply does not exist as far as an operator is concerned. That is the generate-first drift class
  the repo's own rule names, inside the engine that renders the estate's labels.

  The set is now declared ONCE, as an ordered list of name, summary, worked example and the builder
  that produces the implementation, and the other four are readers of it. The safety property
  [ADR-0098](/architecture/decisions/) argues for is unchanged in what it guarantees and stronger in
  how: a function must be in the FuncMap AND the grammar to be usable, and deriving both from one
  declaration makes the two incapable of disagreeing rather than merely expected to. Adding a function
  is still a deliberate act with a test to change, because the closed-set test names the members and
  the committed artifact has to be regenerated.

  Two tests were owed rather than one. The old set was walked in ONE direction (every published name
  parses in a real rule), so a grammar entry with no implementation behind it was the direction nothing
  checked; the converse is now asserted too, and the allowlist may differ from the FuncMap only by the
  named builtins. The docs table is rendered from `docs/src/generated/label.json` (`cmd/labelgen`,
  drift-gated the way the seed and identity artifacts are), and each row's example is EXECUTED through
  the engine on the way into the artifact, so what the table teaches a function does is what the
  function did rather than a claim beside it.

- **The seeded type trees are rendered rather than typed**
  ([#678](https://github.com/hyperscaleav/omniglass/issues/678)). `seed.json` carried eleven shipped
  sets and not the two inheriting classification registries, so the only copy of the `system_type` tree
  in the live docs was a sentence in [core entities](/architecture/core-entities/) naming all eleven
  rows and their nesting, and the `component_type` section taught its taxonomy through examples that
  stopped at "...". The issue's own framing was half right and worth correcting: there was no CLOSED
  hand-written enumeration of `component_type` in the live docs, only an open-ended one, which is a
  different defect (a set that cannot drift because it never claimed to be complete, and also never
  told an operator what ships).

  Both registries now render from the seed, and the render keeps a blank fact BLANK. That is the whole
  point of publishing them: `board` states its own stem and abbrev and leaves its icon to `room`, and a
  table that resolved the chain on the way out would teach the opposite of
  [ADR-0095](/architecture/decisions/)'s first-non-null walk while looking tidier. The location types
  the same bullet list enumerated a second time now point at the guide that already renders them, so
  the estate's shipped shape is stated in exactly one place per set.

  The drift test grew the assertions the shape needs rather than a row count: a child points at its
  parent, a root carries the stem it has no ancestor to inherit, an inheriting row's fact stays empty,
  and no row names a parent declared after it, which would silently drop a subtree out of the rendered
  tree.

- **The roles table stops being a second copy of the seed**
  ([#722](https://github.com/hyperscaleav/omniglass/issues/722)). [Identity and access](/architecture/identity-access/)
  carried a hand-written matrix of the five official roles and what each can do, beside an ASCII
  inheritance diagram, while `seed.json` already renders the same roles with their declared and
  effective permissions resolved through the authorizer's own `rbac` path.
  [#714](https://github.com/hyperscaleav/omniglass/issues/714) had already had to correct one stale
  claim in two places because of it.

  Two more had accumulated, both found by reading `roles.yaml` rather than the page. The `operator`
  row claimed "ack/snooze/resolve alarms", a capability the role holds no permission for and the API
  publishes no route for, while omitting `telemetry:push`, `command:issue`, `file:create,delete` and
  the four catalog grants it does hold. The diagram drew `admin <- owner`, and `owner` inherits
  nothing at all: its permission is `>`, so there is nothing an edge could add. Neither was written
  wrong; both were written once and then left behind by the seed, which is the entire argument for
  rendering.

  The table and the diagram are now one `SeededSet` render, and what the permission strings cannot say
  stays hand-written beside it, per role: why the IAM directories are out of `viewer`'s reach, why an
  admin-sensitive secret is a 404 rather than a 403, why a `deploy` grant reaches exactly one tier, why
  `admin` is not the superuser. That split is the rule the page now follows, and it is the same one
  [the access guide](/guides/admin/access/) already followed.

- **The docs site's OpenAPI plugin comes back inside its declared support range**
  ([#704](https://github.com/hyperscaleav/omniglass/issues/704)). `starlight-openapi@0.26.0` declares
  `astro >=7.0.2` and `@astrojs/starlight >=0.41.0`; the site runs astro 6.4.6 and starlight 0.38.5.
  pnpm warns where npm refuses, so it installed and built anyway, and nothing on the 331 pages we
  publish today was broken by it. Running a plugin outside its support range is still a latent
  failure with a misleading shape: the first page to hit a starlight API that only exists in 0.41+
  would read as a content bug.

  What 0.25 gives up is worth stating exactly, because the ruling asked and the answer is checkable
  rather than assumed: **nothing**. 0.26.0's changelog carries one entry, "Adds support for Astro v7,
  drops support for Astro v6", and that is the whole release. It is not that 0.26's features are
  unused here, it is that 0.26 explicitly dropped the Astro major this project runs. `^0.25.0`
  resolves to 0.25.3, which carries every feature of the 0.25 line (operation snippets, request and
  response snippets, the overview operation list, on-demand rendering) plus its three fixes, and its
  peers (`astro >=6.0.0`, `@astrojs/starlight >=0.38.0`, `@astrojs/markdown-remark >=7.0.0`) are
  satisfied exactly. Moving to astro 7 remains its own slice whenever it is wanted; it is not owed to
  this plugin.

- **The type chain is walked once, in the gateway**
  ([#695](https://github.com/hyperscaleav/omniglass/issues/695)). The naming half of this closed in
  [#702](https://github.com/hyperscaleav/omniglass/issues/702), which made the draft route answer with
  the name itself; what was left was `lib/typechain.ts`, the console's own first-non-null-wins climb of
  both type registries, running against `resolveTypeFacts` in Go for the ICON. A drifted stem produced
  a wrong address; a drifted icon produces a wrong glyph, so this is the same duplication with a much
  smaller consequence, and it was fixed the same way rather than differently.

  Both registry listings now carry `resolved_icon` beside the raw `icon`, and `lib/typechain.ts` is
  deleted along with the inline copy of the same climb that had grown in the Component Types page.
  The cost was weighed rather than assumed: the listing already loads the entire registry in one
  unfiltered query and the handler already indexes it to fill each row's parent name, so the walk is a
  pure pass over rows in hand and adds no query. Resolving PER ROW through the existing
  `ResolveTypeFacts` would have been one query per level per row, turning a one-query list into a
  hundred, so it was not done that way; the field is on the listing only, and a single-row write
  response does not carry it, because nothing reads an icon there.

  The two Go walks that remain are not an accident and are not left on trust. One reads a node at a
  time through a caller's own querier, because the name generator and the label renderer resolve facts
  INSIDE a transaction and must see the write still in flight; the other is a pure pass over a loaded
  listing. A test runs both over every seeded row of both registries and compares, which is what makes
  two walks in one package safe and is the instrument the old cross-language pair never had.

  It also settled a disagreement that was already live rather than hypothetical. The TypeScript walk
  treated an empty-string icon as ABSENT and kept climbing; the gateway's treats it as a value and
  stops, because the column is nullable and null is the "inherit" signal. `PATCH {"icon": ""}` stores
  that empty string today, so the two answered differently for a row an operator could actually
  create. The console now shows what the gateway resolves. Whether that patch should clear the field
  instead of storing a blank is a separate question about the write path, and it is left alone here
  rather than changed under cover of a read-side fix.

- **A registry fork locks before it reads, in a statement of its own**
  ([#709](https://github.com/hyperscaleav/omniglass/issues/709)). A fork writes the whole mutable row
  back rather than patching columns, so it gives up the lost-update immunity a column-wise `coalesce`
  write has: two operators editing different fields of one shipped row at once both compute an image
  from the same starting point, and the second discards the first's field with no error and no audit
  anomaly, both writes in the log. `UpdateComponentType` and `RestoreComponentType` took no lock at
  all, and `UpdateLocationType` and `RestoreLocationType` took one that did not work.

  That second half is the finding, and it was found by writing the test rather than by reading the
  code. [#703](https://github.com/hyperscaleav/omniglass/issues/703) closed this on the location path
  with `for update of lt` on the read that resolves the shadow, and the issue named that as the model
  to copy. Driven by two concurrent forks of one shipped row, `location_type` lost an edit anyway. At
  READ COMMITTED the statement's snapshot is taken when the statement begins, before it blocks, and a
  waiter released by a row that was locked rather than updated gets no EvalPlanQual recheck, so the
  left join it already evaluated still reports the shadow as its stale snapshot saw it. The lock
  serialised the two transactions without making the second read what the first wrote.

  Both registries now take the row's lock through one shared `lockRegistryRow`, in its own statement,
  before the resolving read; the read is then a new statement with a new snapshot taken after the lock
  is held. `ADR-0095`'s adopter contract grows a fourth fact saying so, since this is the half of the
  primitive an adopter cannot infer from the shadow's shape. The test is a paired-fork loop over both
  registries, and it has teeth in both directions: it fails within two rounds against either unlocked
  form, including the one that shipped as the fix.

- **The history windows are bounded by the database's own clock**
  ([#719](https://github.com/hyperscaleav/omniglass/issues/719)). Six read paths built
  `time.Now().UTC().Add(-window)` in the server process and then filtered a `ts` column the database
  stamps with `default now()`: a system's and a location's health transitions, a component's property
  transitions, a component's events, and a component's and a node's logs. Two clocks on one
  comparison, which is the defect [ADR-0108](/architecture/decisions/#adr-0108-settlement-reads-one-clock-and-a-zero-window-is-a-statement-of-intent)
  settled for settlement. Nothing is wrong at a 30-day window, where skew of a few hundred
  milliseconds moves the edge by an invisible amount; what is wrong is that the edge is decided by two
  clocks with nothing bounding the difference, so the same read answers differently against a database
  on another host than against a local one.

  ADR-0108's own answer, reading `select now()` in the transaction, costs one round trip, and it was
  weighed rather than copied. These are read paths, and the three slices before this one
  ([#653](https://github.com/hyperscaleav/omniglass/issues/653),
  [#725](https://github.com/hyperscaleav/omniglass/issues/725),
  [#726](https://github.com/hyperscaleav/omniglass/issues/726)) were spent taking round trips OFF
  them. It did not have to be paid: `Settle` compares in Go and needs `now` as a value, but a history
  read only hands its boundary to a `where`, and a bound can be computed where the data is. The window
  now travels as a **duration** and the query filters `ts >= now() - make_interval(secs => $n)`. One
  clock, six read paths, no extra statement anywhere. A window of zero stays the unwindowed read and
  binds nothing at all.

  The regression test says what it proves and what it does not. Two clocks that agree return the same
  rows as one, so nothing a fixture can build tells them apart on a box where the process and the
  database share a clock, which is every box the suite runs on. What is observable is whether an
  instant crosses the seam at all, so the test captures the statements each read really issued and
  asserts that none of them binds a `time.Time` and that exactly one bounds `ts` against `now()`. A
  process that sends no instant is not the one deciding the boundary, whatever its clock says.

- **The Docker-less test hatch is safe at any parallelism, and the gate's own default is 1**
  ([#662](https://github.com/hyperscaleav/omniglass/issues/662)). `OMNIGLASS_TEST_ADMIN_DSN` points
  the harness at an already-running Postgres instead of starting a testcontainer, for environments
  with no Docker daemon. Two names made it unusable with more than one test binary at a time:
  `og_test_<n>`, from a counter that starts at 1 in every process, and `og_template`, a fixed name
  that the template build drops and recreates, so one binary could drop the template another was
  copying from mid-run. Both now carry a per-process tag (the pid, which separates binaries on one
  host, plus a random half, which separates a shared Postgres reached from two machines and a pid
  reused after a crash), and the template teardown takes a name rather than reading the package's, so
  "the template this binary built" is a property of the call.

  Exposure was zero: nothing sets the variable, and the hatch was already unusable concurrently
  because of the first collision. It is fixed so that raising parallelism on a bigger machine does not
  spring a trap that was set here.

  The gate's own `TEST_PARALLEL` default drops from 4 to 1 in the same change, on the architect's
  ruling. Four concurrent testcontainer Postgres instances plus four link jobs exhausted a WSL2 VM's
  memory and took the whole VM down, not just the run, and a gate that can take the machine with it is
  not a gate. It is a floor rather than a ceiling: the override the comment already documented
  (`make test TEST_PARALLEL=8`) is unchanged, and a machine with the memory for it should raise it.

  One expectation moved, deliberately, and it is the point of the change rather than a casualty of it:
  `provision_test.go` asserted the template's whole literal name, `og_template`. It now asserts a
  prefix carrying this process's pid, and that exactly one database matches it. A test that pins the
  fixed name is a test that pins the defect.

- **An operator acknowledges an alarm (#728).** An alarm records that a human has seen it, and the
  console shows the queue nobody has looked at yet. The `alarm` row gains `acknowledged_at` and
  `acknowledged_by`, `POST /components/{name}/alarms/{id}:acknowledge` sets them behind the new
  **`alarm:acknowledge`** permission (seeded on `operator`), the list read gains an
  `unacknowledged` filter, and the component's Alarms panel gains the action plus an
  unacknowledged indicator.

  The shape of the whole slice comes from one fact read out of the code rather than assumed: an
  alarm's raised state belongs to its **condition** (the one-open invariant is per
  `(component, dedup_key)`, ADR-0075), while an acknowledgement is a fact about a **person**. So the
  acknowledgement is two nullable columns orthogonal to `cleared_at` rather than a `status` enum, and
  `AcknowledgeAlarm` is the one alarm write that does **not** recompute health: acknowledging is not
  fixing, and a transition recorded there would stamp a change at a moment when nothing about the
  estate moved.

  Three decisions were left open by the definition and ruled here, each recorded in
  [ADR-0109](/architecture/decisions/#adr-0109-an-alarm-carries-an-acknowledgement-and-not-a-snooze-or-a-resolve).
  The permission spells its verb out, like every other seeded action, because a permission string is
  operator-facing. A second acknowledgement is **idempotent**, keeping the first person and the first
  time and writing no second audit row, decided in the database by a conditional
  `where acknowledged_at is null` rather than by a read-then-write. And `deploy` does **not** get the
  grant: an alarm resolves scope on the component tier, a location-scoped `deploy` grant reaches no
  component at all (#714), and the shape `deploy` is actually granted in is exactly that one, so the
  capability would have read as given and refused when used.

  **Snooze and resolve were refused rather than deferred.** Snooze suppresses notification and the
  notification registry is unbuilt (#618), so it would have been a column that lies; resolve is either
  the existing clear under a second name or an unspecified concept. The three-verb permission string
  that had sat in the test fixtures and in the identity-access page for most of a year turned out to
  be neither seeded nor enforced by anything, and the fixtures that carried it now use vocabulary no
  route will ever stamp.

  Two expectations moved. `internal/rbac/rbac_test.go` asserted the read floor against
  `alarm:ack`, a permission that did not exist; it now asserts the same floor against the real
  `alarm:acknowledge`. `internal/seed/seed_shrink_test.go` used `alarm:ack,snooze,resolve` as its
  retired grant and now uses `widget:frobnicate,polish`, since the mechanic under test is the
  upsert's authority and borrowing a real resource's vocabulary for it is what left phantom prior art
  in the repo in the first place. `TestALocationGrantOfDeployReachesOneTier` gained `alarm` in its
  matrix, so the `deploy` question resurfaces on its own if the cross-tier expansion (#10) lands.

  One divergence recorded and not fixed: the identity-access page's three-way status split says a
  target the caller can read but not act on is a **403**, and every scoped route in the platform
  answers **404** there, this one included. Fixing it for this route alone would have made it the
  only route that answers differently from its own siblings, so it is one shared refusal primitive
  in [#736](https://github.com/hyperscaleav/omniglass/issues/736) and the page now marks the branch
  as design.

- **The last stored function retires, and a principal's identifier becomes the gateway's answer
  (#564).** `principal_label(uuid)` was a stored SQL function, `coalesce(human.username,
  service.label)`, and it was two defects wearing one name: it put the platform's answer to "what
  names this principal" in the database, which is the one place this repository says logic never
  lives, and it called the answer a **label** when both of its branches return an identifier. No
  test named it, so it could have returned anything and the suite would have agreed.

  The answer now lives in `internal/storage/principal_ident.go`, which names the two sources once
  and renders every shape a statement can need from it, so a caller picks a shape and never a
  column. A read over many rows LEFT JOINs the sources and folds them in Go. Two positions cannot
  join: `alarmCols` is read by an `UPDATE ... RETURNING` and `RETURNING` cannot left-join, and the
  audit insert runs inside the caller's transaction on every operator write, where a Go fold would
  cost a second round trip and the alarm write path pins its statement count as the exact equation
  `12 + 5*slots + 4*locations` (#674) that counts that insert. Both of those render the sources as
  correlated sub-selects, and both are bounded reads. Which shape is measured rather than assumed:
  on a 500-member group roster the sub-select shape projected AND sorted on costs 3011 shared buffer
  hits and 2000 index searches where the join costs 18. Moving the policy out of the database was
  the point; paying a round trip on every operator write, or two thousand index probes on a roster,
  was not part of it, and the pinned equation is unmoved.

  Only the ORDER is written twice, so `principal_ident_test.go` is the invariant between the two
  shapes: it drives a human, a service account, a node and an unknown id through both against a real
  database. A `node` stays out of the resolution exactly as the dropped function had it, so no audit
  row changes what it says. Recorded as
  [ADR-0110](/architecture/decisions/#adr-0110-a-principals-identifier-is-the-gateways-answer-not-a-stored-functions),
  with `principal_label` appended to the vocabulary denylist.

- **A service account's identifier is a name, and it is unique (#563).** `service` has exactly two
  columns and the second one is what identifies the row: the username analogue for `kind=service`,
  the only operator-visible handle it has. It was called `label`, and it was on the wire three lines
  from the human body's `display_name`, so two different concepts read as one. It is now
  `service.name`, `name` on the wire, and **Name** in the console.

  **The uniqueness question was answered rather than inherited.** The column carried no index and no
  constraint; it now carries `service_name_key`, matching `human_username_key` and `node_name_key`.
  Not for symmetry: the string is denormalized as bare text into `audit_log.actor_username` at write
  time and into an alarm's acknowledgement on every read, and a duplicate makes both unresolvable
  after the fact with no uuid left beside the text. The migration copes with duplicates rather than
  assuming there are none, in the foundation / backfill / floor shape: the oldest row of each
  duplicated set keeps the name (`principal.id` is uuidv7, so it sorts by creation time) and every
  other is suffixed with its own principal id, unique by construction. The harness migrates an empty
  database, so running the chain forward can never exercise that sweep; a test stands the schema
  between the rename and the floor, writes three accounts under one name, and runs the extracted SQL
  twice, then lands the constraint on the result.

  **The identity declaration moved with the column, and grew the guard that would have caught it.**
  `service` was declared `ShapeIDOnly`, which publishes "nobody names it", and that was believable
  only while the identifier was spelled `label`. It is now `ShapeHumanNotAKey` with its reason, and
  a new guard reads the generated schema facts and fails any `ShapeIDOnly` table that carries a
  `name`.

  **The clearest illustration of why the word had to move is the group roster.** It read
  `coalesce(h.display_name, s.label, '')` into a single field, a human's friendly string falling
  through to a service account's identifier; after the rename the same chain reads
  `coalesce(h.display_name, s.name, '')` and the mistake is on the page. It is now two fields, the
  identifier (`name`, resolved by the gateway's `principalIdent`) and the label (`display_name`,
  which only a human has a column for). An API test had been asserting a service account's
  identifier out of a field called `display_name`; it now asserts it out of `name` and asserts the
  display name is empty. Recorded as
  [ADR-0111](/architecture/decisions/#adr-0111-a-service-accounts-identifier-is-a-name-and-it-is-unique).

  **Breaking wire change** on two read shapes (`svcBody.label` to `name`, and the roster's
  `username` to `name`). No CLI flag moves: neither is a request field.
- **A generated flag carries the schema's own type**
  ([#711](https://github.com/hyperscaleav/omniglass/issues/711)). Every body flag the CLI generator
  emitted was a string, and every non-string field was coerced at run time by `jsonOrString`. So the
  spec said `integer`, the flag said `string`, the generated CLI reference published `string`, and an
  operator who typed a word learned about it from the server's 422 after a round trip that had already
  authenticated. That is the generate-first drift class the repo's own rule names, sitting inside the
  generator: a fact the spec states, restated by hand as something else.

  `cmd/cligen` now maps the property's type to the flag that carries it, so `--settle-window-seconds
  soon` is refused by the shell before a request is issued, `--ttl-days` is an `int` and
  `--sensitive` a `bool` in `--help` and in the reference. A structured field keeps ONE string flag
  parsed as JSON, and that is a ruling rather than a leftover
  ([ADR-0112](/architecture/decisions/#adr-0112-a-generated-flag-carries-the-schemas-type-and-a-structured-field-carries-json)):
  a nested value has no shell-native flag type, and `null` has to stay sendable because it is what
  clears a field named in `update_mask`. A nullable STRING is the single exception, since this API
  clears a string with the empty string.

  The tests drive the REAL generated tree against a canned server rather than the generator's model:
  a word for an integer flag is refused with the server as a tripwire that fails the test if a request
  was issued at all, `--settle-window-seconds 15` arrives on the wire as the JSON number 15, and both
  spellings of the object case are pinned (`--name-rule '{...}'` and the `--name-rule null` that
  clears it under the mask), so a later move to per-leaf flags cannot quietly take the second away.

  One documented line broke, and the guard that found it is the point. A bool flag reads
  `--propagates false` as a positional argument, so `--required true` in the CLI guide could no longer
  work; the docs flag check now fails on a bool flag handed a space-separated `true` or `false`. Two
  expectations moved in `cmd/cligen/main_test.go`, both on the boolean: `propagates.JSON` was `true`
  and is `false`, and the rendered body line was `body["propagates"] = jsonOrString(fPropagates)` and
  is `body["propagates"] = fPropagates`.

  **The issue's own example was wrong and is corrected here.** It names `expected_ordinal` as one of
  the two fields that surfaced this; the #702 review replaced that field with `expected_name` and
  `internal/docslint` refuses the word, so there is no integer field by that name to cover.
  `settle_window_seconds` is the integer under test in its place.

- **The label data map is declared once, and the docs render it**
  ([#729](https://github.com/hyperscaleav/omniglass/issues/729)). One paragraph away from the function
  table [#701](https://github.com/hyperscaleav/omniglass/issues/701) had just made generated, the keys
  a rule may READ were still typed out: in `core-entities`, in two `label_rule` API field
  descriptions, and a third time in the map literals that build them. This arc changed that map three
  times (#682 built it, #685 added the placement facts, #693 changed what `Ordinal` reports) and each
  time the prose had to be found and hand-edited.

  Each kind now declares its map ONCE, as an ordered list of key, summary and the accessor that
  produces the value, and `labelData` builds the map by ranging that declaration. The docs render
  `docs/src/generated/labeldata.json` (`cmd/labelgen` writes it beside the rule language's own
  artifact, drift-gated the same way), one table per entity kind rather than a union, because the
  differences between the three are the design: a location has no product, no vendor and no placement
  fact, and the reasons live in the declaration's own comment now instead of in three places.

  **The render is per kind and it carries a description per key**, because one key needed one. Since
  #693 `Ordinal` is not the stored ordinal: it is the number the row's NAME shows, empty when an
  operator named the row or when the mint suppressed the first of its stem, and a bare list of key
  names taught the opposite of that.

  **A live drift fell out of doing it.** The two `label_rule` descriptions, which are the console's
  field help and a row in the generated CLI reference, enumerated the map as it stood before #685: an
  operator was taught seven keys where a rule could read nine, and `LocationLabel` and
  `SystemTypeLabel`, the two the epic's worked example is built from, were missing from the surface an
  operator meets them on. A Huma description is a struct tag and cannot be built from the declaration
  at run time, so a test reads the generated OpenAPI and holds every such description to the declared
  key set and to the engine's own function names.

- **The decision log stops documenting a lock key the code left behind**
  ([#717](https://github.com/hyperscaleav/omniglass/issues/717)).
  [ADR-0056](/architecture/decisions/#adr-0056-every-foreign-key-stores-a-primary-key) carved health
  out of the uuid conversion and stated why: its advisory lock hashed `health/<kind>/<name>`, so a
  mixed currency would hash two keys for one owner and silently stop serializing. The lock has hashed
  the row's **id** since the identity epic ([#627](https://github.com/hyperscaleav/omniglass/issues/627))
  landed its addressing slice ([#647](https://github.com/hyperscaleav/omniglass/issues/647)).

  This is more than a stale sentence, and the connection is the point: a name-keyed lock partitions
  an estate only while names partition it, and that same epic scoped a location's name uniqueness to
  its **placement**, so two rooms under different buildings may both be `415a`. The keying scheme
  stopped being safe at exactly the moment the epic made names non-unique. The code moved and the log
  did not, so the log was documenting a scheme that would be a live defect if anyone implemented it
  from the page.

  The entry is amended in place rather than renumbered, the original bullet left standing with a
  forward pointer, since the log is append-only and its argument (one currency per lock key) is the
  one that reversed its conclusion. The [health](/architecture/health/) page needed no change: it has
  said "owners are locked by id, in one order" since
  [#670](https://github.com/hyperscaleav/omniglass/issues/670) moved the lock ORDER for the same
  reason. The build log's own copy of the old claim is left exactly as it shipped, which is what this
  page is for.

  The fact now has a test rather than only a paragraph. The key moved into one named function
  (`healthLockKey`) and a unit test asserts what the amendment claims: two same-named rooms do not
  share a lock, one owner keys consistently, and the owner kind is part of the key.

- **The console's validation attributes were decorative, so the rule moved to TypeScript**
  ([#724](https://github.com/hyperscaleav/omniglass/issues/724)). Found while closing
  [#718](https://github.com/hyperscaleav/omniglass/issues/718), whose brief asked for a `min="0"` on
  an input that had carried one since [#411](https://github.com/hyperscaleav/omniglass/issues/411)
  and had never fired once. The browser enforces `required`, `min`, `max` and `pattern` on a real form
  **submission**, and this console performs none on the paths an operator uses: a Drawer's action rail
  is drawn by the shell and portaled outside the `<form>`
  ([ADR-0054](/architecture/decisions/#adr-0054-the-shell-owns-a-panels-action-rail-the-body-registers-and-never-draws)),
  a blade has no `<form>` at all, and the inline editors save from an `onClick`.

  **The audit decided the ruling rather than following it**: 21 attributes on 24 rendered controls,
  17 sites on a surface with no form submission, and the remaining four (three on Login, one on the
  forced password change) sitting in forms whose submit button is `disabled` in exactly the states
  native validation would have refused. **Zero of the twenty-four could ever fire**, so a reader could
  not tell a live attribute from a decorative one because there were none of the first kind.

  So the rule is one vocabulary
  ([ADR-0113](/architecture/decisions/#adr-0113-a-validation-rule-is-typescript-and-a-native-constraint-attribute-is-not-one)):
  a pure function judges the typed value, the surface renders its message inline beside the field, and
  the binding's `disabled` / `valid` refuses the submit, which reads the same on a form and on a blade.
  Making the attributes live instead (`form.requestSubmit()` from the rail) would have covered the
  Drawers only, left every blade needing this decision anyway, and meant undoing the disabled gate so
  an unstyled browser bubble could refuse in place of the inline message.

  One rule was genuinely missing rather than duplicated. The token drawer's `min="1" max="365"` had no
  TypeScript behind it, so a 400-day token was refused by the server's 422 after a round trip; it is
  `tokenTtlError` now, asserted through the drawer's own rail button rather than by calling the
  validator. `aria-required` replaces `required` on the eleven fields that carried it, since a screen
  reader should still hear the fact; `type="email"` and `type="number"` stay, because an input type
  carries keyboard, autofill and spinner behaviour and is not a claim about refusal.
  `web/src/validation-guard.test.ts` scans every `.tsx` and fails on a native constraint, with a
  self-check that the scanner can still find one (its first version could not: a JSX attribute written
  after an arrow-function handler defeats any regex that stops at the first `>`).
