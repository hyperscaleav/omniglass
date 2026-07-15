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
generated self-scoped commands (`omniglass auth me`, `auth update-profile`,
`auth change-password`, `me setAvatar` / `removeAvatar`); an administrator manages other
principals, roles, groups, secrets, and variables through the resource commands, all covered
in the [admin guide](/guides/admin/) and listed in full in the [CLI reference](/reference/cli/).

## Tags

The [tag](/architecture/tags/) commands split along the governance line. The `tag` resource covers the
**key vocabulary** (minting, editing, and deleting keys, plus the global default binding), while binding a
value onto an entity is a **custom method on the entity** (`component set-tag` and friends), so it needs
only the write the operator already holds on that entity. `effective-tag` reads the resolved cascade onto
one component.

```sh
omniglass tag list                                  # the governed key vocabulary
omniglass tag create --name environment             # mint a key (tag:create, admin)
omniglass tag create --name rack_position --applies-to '["location"]' --propagates=false
omniglass tag update environment --applies-to '["component","system"]'
omniglass tag setGlobal environment --value prod    # a tenant-wide default (tag:update)
omniglass tag clearGlobal environment
omniglass tag delete environment                    # cascades its bindings

omniglass component setTag codec-1 --key environment --value dev    # component:update
omniglass component listTags codec-1                # the bindings set directly on the component
omniglass component removeTag codec-1 --key environment
omniglass system setTag east-auditorium-av --key environment --value prod
omniglass location setTag hq --key environment --value staging

omniglass effective-tag list codec-1                # the cascade resolved onto a component
```

Binding is a custom method on the entity (`component setTag`), like the principal lifecycle verbs, so it
stays clear of the top-level `tag` commands. A key name is a normalized lowercase identifier (minting a
bad name is a 422). `--applies-to` is an entity-kind allow-list passed as a JSON array
(`'["component","system"]'`; empty means universal), checked when a value is bound. `--propagates`
defaults true (the value cascades to descendants); `--propagates=false` binds a flat per-entity value
that resolves only on its own entity. Resolving onto a component **unions** keys and **overrides** values
most-specific-wins down the cascade.

## Component makes

The [component make](/architecture/core-entities/#catalog-reference-data-component_make) commands cover
the manufacturer registry: a flat, official-vs-custom catalog on the same pattern as the `type`
registries. `make:read` sits on the viewer floor; the three writes (`make:create`, `make:update`,
`make:delete`) are admin-gated.

```sh
omniglass component-make list                                       # the manufacturer registry
omniglass component-make create --id barco --display-name Barco \
  --icon monitor --support-phone "+1-555-0100" --website https://www.barco.com
omniglass component-make get barco
omniglass component-make update barco --support-phone "+1-555-0199"
omniglass component-make delete barco                                # refused (422) if official
```

A seed-owned (**official**) make, for example `crestron` or `biamp`, is read-only: `update` and `delete`
both 422. `website` is validated to an `http`/`https` scheme on write; any other scheme (for example
`javascript:`) is a 422.

## Component models

The [component model](/architecture/core-entities/#catalog-reference-data-component_model) commands
cover the product catalog: a specific make + model product, one layer down from `component-make`, on
the same official-vs-custom pattern. `model:read` sits on the viewer floor; the three writes
(`model:create`, `model:update`, `model:delete`) are admin-gated.

```sh
omniglass component-model list                                      # the product catalog
omniglass component-model create --id tsw-1070 --display-name TSW-1070 \
  --make-id crestron --model-number TSW-1070-B-S --family TSW
omniglass component-model get tsw-1070
omniglass component-model update tsw-1070 --family "TSW gen2"
omniglass component-model delete tsw-1070                            # refused (422) if official
```

A seed-owned (**official**) model is read-only: `update` and `delete` both 422. `make_id` is set at
create and is not patchable after. Front/back product photos take a file id (`--front-image-id` /
`--back-image-id`), uploaded through the [Files](/guides/admin/files/) commands first; an unknown
`make_id` or image id is a 422. Deleting a `component-make` still referenced by a model is now refused
too (409), the referential guard deferred from the make registry.

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
