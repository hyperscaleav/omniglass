---
title: The CLI
description: "The omniglass CLI: a generated client of the HTTP API, with a stable seam for hand-written commands."
---

The `omniglass` binary is both the server and the client. Its data commands are
**generated from the OpenAPI** (`make gen`, via `cmd/cligen`), so the CLI cannot drift
from the API: a new route is a new command on the next regeneration. A small set of
commands (the run modes and the trusted bootstrap) are hand-written and compose with the
generated tree on the same root.

This page is the mental model and the setup. For the exhaustive, generated list of every
command, its flags, and an example, see the **[CLI reference](/reference/cli/)**.

## Running the full stack locally

`make dev` brings up the whole stack for a browser session: a dev Postgres (docker
compose, matching the default DSN), the migrations and boot seed, a bootstrapped `dev`
owner whose token is printed once, a dev example estate (a few campuses with rooms, a few
sign-in-able users, and their grants, so the console is not empty), and the server with
the operator console at `http://localhost:8080/web`. Ctrl-C stops the server; `make down` stops Postgres (the
named volume persists data between runs; `docker compose down -v` wipes it and re-mints a
token next run). `make up` / `make down` manage just the database. Tests never touch this
stack: they spin their own ephemeral Postgres via testcontainers.

## Connecting

Every generated command is a client of a running server and takes two shared flags, each
with an environment default:

| Flag | Env | Default |
|---|---|---|
| `--server` | `OMNIGLASS_SERVER` | `http://localhost:8080` |
| `--token` | `OMNIGLASS_TOKEN` | (none) |

The token is a bearer credential (mint the first one with `omniglass bootstrap`; see
[Authentication](#authentication) below). The server enforces the same capability and scope
for the CLI as for any caller: the CLI is just another client, with no privileged path.

```sh
export OMNIGLASS_SERVER=https://omniglass.example.com
export OMNIGLASS_TOKEN=ogp_...
omniglass location list
omniglass location create --name hq --location-type campus
omniglass location get hq
```

Output is JSON. A non-2xx response prints the server's error body and exits non-zero, so
the CLI is safe in scripts.

## Authentication

There are two ways to authenticate, both accepted on every request. A **bearer token** in
the `Authorization` header (the `--token` flag above) is the path for services and the CLI.
A **username and password** is the path for a human at the web console: the server verifies
it and sets an httpOnly session cookie. The CLI itself always uses a bearer token.

The first owner is created directly against the database with `bootstrap` (the trusted lane,
no running server needed):

```sh
# Mints a bearer credential (the token is printed once) and, with --password, a password
# credential (argon2id) so the owner can sign in to the console. This is a trusted direct-DB
# lane, so it is exempt from the password policy (unlike the console/API paths). The bootstrap
# token expires after --ttl (default 90 days, hard maximum 365 days).
omniglass bootstrap ops --password 'set-a-strong-one' --email ops@example.com --display-name "Ops Lead"

# Mint a fresh bearer token for an existing user (direct-DB, owner lane). A --description
# (required) names what the token is for. Every credential is time-bounded: the token expires
# after --ttl (default 90 days, hard maximum 365 days; a --ttl above the cap is an error). A
# web-login session cookie has its own, shorter fixed lifetime.
omniglass token ops --description 'ci pipeline'
omniglass token ops --description 'nightly backup' --ttl 720h   # a 30-day token

# A signed-in user can mint its own token over the API (the console Create token action), which
# returns the secret once: omniglass auth create-token --description 'my laptop cli'.

# Set or rotate a user's password (direct-DB, owner lane; also policy-exempt as the recovery path).
# A break-glass reset also revokes the user's live SESSIONS, so a stolen login stops at once; API
# tokens are kept unless --revoke-tokens is given (a full lockout of a compromised account).
omniglass set-password ops 'set-a-strong-one'
omniglass set-password ops 'set-a-strong-one' --revoke-tokens   # full lockout: sessions and tokens
```

Once a server is running, a signed-in principal manages **its own** account through the
generated self-scoped commands (each edits only the caller's own profile):

```sh
omniglass auth me                                    # your principal, permissions, and grants
omniglass auth update-profile --display-name "Ops Lead"
omniglass auth change-password --current-password 'orange-boat-42x' --new-password 'purple-canyon-7'
omniglass auth set-avatar --image-base64 "$(base64 -w0 me.jpg)"   # set your profile picture
omniglass auth remove-avatar                            # clear it, falling back to initials
omniglass principal principal avatar list                                # read your picture back as { image_base64 }
```

## Collection commands

The [collection](/architecture/collection/) surface regenerates into four command groups:
`node`, `interface`, `task`, and `reachability`. They follow the same derivation as every
other resource (`POST /interfaces` is `interface create`, `GET /tasks/{id}` is `task get
<id>`), so nothing here is special-cased. All examples require the matching permission on
the running server.

Register and enroll an edge node (the day-one handshake):

```sh
omniglass node list
omniglass node create --name edge-hq --display-name "HQ Edge Node" --location hq-west --description "HQ network closet"   # needs node:create (all-scope)
omniglass node get edge-hq
omniglass node update edge-hq --display-name "HQ Edge" --location hq-west   # needs node:update; the name is immutable
omniglass node delete edge-hq   # needs node:delete; decommissions the node (cascades its interfaces, tasks, and enrollment)
```

Every command above is new in practice, not only in the docs: a hand-written `node` run
mode occupied the same name as the generated `node` group, so cobra resolved `omniglass
node list` to the daemon and it failed asking for `--token`. The run mode is now
`omniglass node run`, a verb beside the others:

```sh
omniglass node run --name edge-hq --token ogp_...   # the edge daemon: claim, pull the worklist, heartbeat
```

The node-facing `claim` exchange is public (the enrollment token is the authentication), so
a node presents its name and token to receive its NATS credential:

```sh
omniglass node claim --name edge-hq --token ogp_...
```

:::note[Thin cut: `node enroll`]
`omniglass node enroll` regenerates as a command, but the `{name}` path parameter of the
`:enroll` custom method is not yet bound (it takes no positional argument), so it cannot
target a specific node from the CLI today. Mint an enrollment token from the console (the
Nodes page) or against the API until that binding lands. `node claim`, which carries its
name in the body, works as shown.
:::

Author a reachability check by **creating an interface** (its poll task is derived
automatically):

```sh
# An interface owned by a component, placed on a node, with its probe target in params.
# It is named by its protocol: --type is the interface_type, there is no --name flag.
omniglass interface create \
  --type tcp --component disp-1 --node edge-hq \
  --params '{"target":"10.0.0.1:22"}'                          # needs interface:create

omniglass interface list
omniglass interface get <id>                                    # interfaces are addressed by id
omniglass interface update <id> --node edge-hq --params '{"target":"10.0.0.2:22"}'
omniglass interface delete <id>                                 # refused (409) while its task references it

# The poll task is derived from the interface, so the task surface is read-only.
omniglass task list
omniglass task get <id>
```

The four built interface types are `icmp`, `tcp`, `ssh`, and `http`, and an interface is
**named by its protocol** (the `--type`), unique within its component. An interface `update`
changes only its node placement and params. A **task** is **derived** when its interface is
created, so there is no `task create`, `update`, or `delete`; its placement follows the
interface's. A node purge cascades its interfaces and their derived tasks.

Read a component's composed reachability (the verdict, the probe-layer signals, and the
recent transitions the availability strip draws):

```sh
omniglass component component reachability list disp-1                              # needs component:read
```
`--image-base64` takes a plain base64 string, not a file path (base64-encode the image
yourself, as the `$(base64 …)` above does); the server accepts JPEG, PNG, or WebP and
normalizes it to a 256x256 JPEG. An administrator manages **any** principal's picture with
`omniglass principal setAvatar <id> --image-base64 …` and `omniglass principal removeAvatar <id>`
(gated by `principal:set-avatar`), reading one back with `omniglass principal principal avatar list <id>` (gated by
`principal:read`). A principal with no picture is a 404.

## Secrets

The [secret](/architecture/variables/) commands are generated like every other resource. `secret`
covers the encrypted values and `type secret` lists the shape registry. Output is masked JSON, the same as the console; plaintext lives
behind `reveal`, which the server audits and which only admin and owner may call.

```sh
omniglass secret-type list                           # the shape registry (snmp-community, basic-auth)
omniglass secret list                               # the all-scope admin directory (masked fields)
omniglass secret create --name core-snmp --secret-type snmp-community \
  --owner-kind location --owner hq --fields '{"community":"public"}'
omniglass secret update <id> --fields '{"community":"s3cret"}'   # an omitted field keeps its value
omniglass secret reveal <id>                        # audited plaintext decrypt (secret:reveal)
omniglass secret delete <id>
```

There is no command that resolves the cascade onto one component for either secrets or
variables: the resolvers exist in the Storage Gateway but no API route exposes them, so no
command generates ([#359](https://github.com/hyperscaleav/omniglass/issues/359)). Only
`component effective-tag list` has its route today.

`--owner-kind` is one of `platform | location | system | component`; `--owner` names the owning entity
and is omitted for a `platform` secret (the install-wide tier, which needs an all-scope grant plus
`platform:create`). Field maps pass as a JSON object to `--fields`, validated against the type's shape.

## Variables

The [variable](/architecture/variables/) commands are generated the same way. `variable` covers the
plaintext values. There is no reveal: the value is shown in the clear.

```sh
omniglass variable list                             # the all-scope admin directory
omniglass variable create --name poll_interval --value-type int \
  --owner-kind system --owner east-auditorium-av --value 30
omniglass variable create --name retry --value-type json --owner-kind platform \
  --value '{"retries":3,"backoff":"1s"}'
omniglass variable update <id> --value 60           # validated against the fixed value_type
omniglass variable delete <id>
```

`--value-type` is one of `string | int | float | bool | json`. `--value` is **parsed as JSON**, so a
bare `30`, `true`, or `{"k":"v"}` sends the number, the boolean, or the object; a bare word like `HDMI1`
falls back to a string, so the common case needs no quoting. A string value that would otherwise parse
as JSON (`30`, `true`) is quoted to force a string: `--value '"30"'`. (`secret create --fields` parses
the same way.)
generated self-scoped commands (`omniglass auth me`, `auth update-profile`,
`auth change-password`, `auth set-avatar` / `removeAvatar`); an administrator manages other
principals, roles, groups, secrets, and variables through the resource commands, all covered
in the [admin guide](/guides/admin/) and listed in full in the [CLI reference](/reference/cli/).

## Tags

The [tag](/architecture/tags/) commands split along the governance line. The `tag` resource covers the
**key vocabulary** (minting, editing, and deleting keys, plus the install-wide `platform` binding), while binding a
value onto an entity is a **custom method on the entity** (`component set-tag` and friends), so it needs
only the write the operator already holds on that entity. `effective-tag` reads the resolved cascade onto
one component.

```sh
omniglass tag list                                  # the governed key vocabulary
omniglass tag create --name environment             # mint a key (tag:create, admin)
omniglass tag create --name rack_position --applies-to '["location"]' --propagates=false
omniglass tag update environment --applies-to '["component","system"]'
omniglass tag setPlatform environment --value prod  # an install-wide default (tag:update + platform:update)
omniglass tag clearPlatform environment
omniglass tag delete environment                    # cascades its bindings

omniglass component setTag codec-1 --key environment --value dev    # component:update
omniglass component listTags codec-1                # the bindings set directly on the component
omniglass component removeTag codec-1 --key environment
omniglass system setTag east-auditorium-av --key environment --value prod
omniglass location setTag hq --key environment --value staging

omniglass component component effective-tag list codec-1                # the cascade resolved onto a component
```

Binding is a custom method on the entity (`component setTag`), like the principal lifecycle verbs, so it
stays clear of the top-level `tag` commands. A key name is a normalized lowercase identifier (minting a
bad name is a 422). `--applies-to` is an entity-kind allow-list passed as a JSON array
(`'["component","system"]'`; empty means universal), checked when a value is bound. `--propagates`
defaults true (the value cascades to descendants); `--propagates=false` binds a flat per-entity value
that resolves only on its own entity. Resolving onto a component **unions** keys and **overrides** values
most-specific-wins down the cascade.

## Component classification catalogs

The [component-classification catalog](/architecture/core-entities/#catalog-reference-data-vendor-driver-capability)
commands cover the `vendor`, `driver`, and `capability` registries: flat, official-vs-custom catalogs on
the same pattern as the `type` registries. Each resource's `:read` sits on the viewer floor; the three
writes (`:create`, `:update`, `:delete`) are admin-gated.

A **vendor** names an organization, carrying a `--kind` of `manufacturer`, `integrator`, or `developer`
(default `manufacturer`):

```sh
omniglass vendor list                                               # the vendor registry
omniglass vendor create --id barco --display-name Barco --kind manufacturer \
  --icon monitor --support-phone "+1-555-0100" --website https://www.barco.com
omniglass vendor get barco
omniglass vendor update barco --support-phone "+1-555-0199"
omniglass vendor delete barco                                       # refused (422) if official
```

A **driver** names the implementation that gets, emits, or sets a product's signals, with an optional
`--version`. A **capability** names what a component can do:

```sh
omniglass driver list                                               # the driver registry
omniglass driver create --id barco-snmp --display-name "Barco SNMP" --version 1.0.0
omniglass driver update barco-snmp --version 1.1.0
omniglass driver delete barco-snmp                                  # refused (422) if official

omniglass capability list                                           # the capability registry
omniglass capability create --id projector --display-name Projector
omniglass capability delete projector                               # refused (422) if official
```

A seed-owned (**official**) row, for example the `crestron` vendor or the `microphone` capability, is
read-only: `update` and `delete` both 422. A vendor's `website` is validated to an `http`/`https` scheme
on write; any other scheme (for example `javascript:`) is a 422.

## Products

The [product](/architecture/core-entities/#catalog-reference-data-product) commands cover the product
registry: the concrete **SKU** that ties the vendor, driver, and capability catalogs together, and the
thing a `component` points at. `product:read` sits on the viewer floor; the three writes
(`product:create`, `product:update`, `product:delete`) are admin-gated.

A product names its **kind** (`device`, `app`, `service`, or `vm`, default `device`), optionally its
**vendor**, **driver**, and a **parent product** it is a variant of, and the **capabilities** it
provides (a JSON array of capability ids):

```sh
omniglass product list                                              # the product registry
omniglass product create --id barco-ub12 --display-name "Barco UB12" --kind device \
  --vendor-id barco --driver-id barco-snmp --capabilities '["projector"]'
omniglass product get barco-ub12
omniglass product update barco-ub12 --capabilities '["projector","speaker"]'  # replaces the whole set
omniglass product delete barco-ub12                                 # 422 if official, 409 if a component points at it
```

A seed-owned (**official**) product, for example `cisco-room-bar`, is read-only: `update` and `delete`
both 422. A product still referenced by a **component** (`component.product_id`) cannot be deleted (409);
an unknown vendor, driver, parent, or capability id is a 422.

## Standards

The [standard](/architecture/core-entities/#catalog-reference-data-standard) commands cover the
system-side counterpart of a product: the **blueprint a system conforms to**. `standard:read` sits on the
viewer floor; `standard:create`, `standard:update`, and `standard:delete` are admin-gated.

```sh
omniglass standard list                                             # the standard catalog
omniglass standard create --id lecture-hall --display-name "Lecture Hall" \
  --parent-standard-id classroom                                    # a variant of an existing standard
omniglass standard get lecture-hall
omniglass standard delete lecture-hall                              # 409 if a system still conforms to it
```

Unlike a seeded product, the **shipped standards are `official: false`**: they are forked from an in-code
template once, with no inheritance, so they are yours to rename, re-parent, or delete, and the boot seed
installs one **only if absent** rather than reasserting over your edit. The same holds for the shipped
location types. See [the seed model](/architecture/core-entities/#the-seed-model-forked-templates-versus-canonical-catalogs).

## Property contracts and values

A classifier **declares** which properties its instances carry; an instance **sets** a value. Both sides
are the same three verbs, and the contract commands hang off the classifier that owns them:

```sh
omniglass product property list cisco-room-bar                         # a product's contract
omniglass standard property list huddle-room                           # a standard's contract
omniglass location-type property list room                             # a location type's contract
omniglass standard property update huddle-room room_capacity --default-value 6 --required true
omniglass standard property delete huddle-room room_capacity        # systems keep any value they set

omniglass component property list dsp-boardroom-3                      # the effective read
omniglass system property list boardroom                               # same shape, system side
omniglass location property list east-campus                           # same shape, location side
omniglass system property update boardroom room_capacity --value 12    # idempotent
omniglass system property delete boardroom room_capacity             # falls back to the contract default
```

The read resolves the classifier's contract against the instance's own values, so a **one-off system**
(one conforming to no standard) and a **productless component** still resolve, to their off-contract
values alone. The value commands are **scope-injected**: an instance outside your read scope is a
non-disclosing 404 on the read and on the write. The registry and its contract share one noun:
`omniglass location-type list` is the registry, `omniglass location-type property list <id>` its contract.

## Generated versus hand-written

- **Generated** (`internal/cli/api_gen.go`, do not edit): one command per API operation.
  The resource and verb come from the AIP-style path (`POST /locations` is `location
  create`, `GET /locations/{name}` is `location get <name>`, a `:verb` custom method is
  `<resource> <verb> <id>`, so the principal lifecycle is `principal disable <id>`,
  `principal archive <id>`, `principal restore <id>`, and `principal purge <id>`); path
  parameters are positional args, the request body is
  `--flags`, and OpenAPI query parameters become optional `--flags` (a set flag is
  appended to the request query string, an unset one keeps the server default), so
  `principal list --include-archived --kind service` filters the listing. A principal
  `<id>` argument accepts either the uuid or a human's username (`omniglass principal
  archive alice`), resolved by the server, so you rarely need to look a uuid up first.
  `--help` plus the example come from the operation's summary and description.
- **Hand-written** (`internal/cli/api_hooks.go` and the run-mode files): the client
  runtime the generated tree calls, plus commands that are not API operations, the
  `server` and `migrate` run modes and the trusted direct-DB owner lane (`bootstrap`,
  `token`, `set-password`).

To add a hand-written command, write a `newXxxCmd()` returning a `*cobra.Command` and add
it in `newRoot`, exactly as `bootstrap` does. Regenerating the API commands never touches
it.
