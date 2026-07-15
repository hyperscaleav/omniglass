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
omniglass me setAvatar --image-base64 "$(base64 -w0 me.jpg)"   # set your profile picture
omniglass me removeAvatar                            # clear it, falling back to initials
omniglass avatar list                                # read your picture back as { image_base64 }
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
omniglass node create --name edge-hq --description "HQ network closet"   # needs node:create (all-scope)
omniglass node get edge-hq
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

Author a reachability check (an interface plus a poll task over it):

```sh
# An interface owned by a component, placed on a node, with its probe target in params.
omniglass interface create \
  --name disp-1-tcp --type tcp --component disp-1 --node edge-hq \
  --params '{"target":"10.0.0.1:22"}'                          # needs interface:create

omniglass interface list
omniglass interface get disp-1-tcp
omniglass interface update disp-1-tcp --node edge-hq --params '{"target":"10.0.0.2:22"}'
omniglass interface delete disp-1-tcp                          # refused (409) while a task references it

# A poll task over the interface, put on the node's worklist.
omniglass task create --interface disp-1-tcp --mode poll        # needs task:create; --enabled defaults to true
omniglass task list
omniglass task update <id> --display-name "HQ display ping"     # id is content-addressed; interface/mode are fixed
omniglass task delete <id>
```

The two built interface types are `icmp` and `tcp`. An interface `update` changes only its
node placement and params; a task `update` changes only its display name, enabled toggle,
node, and spec (the interface and mode form its content-addressed id and are fixed).

Read a component's composed reachability (the verdict, the probe-layer signals, and the
recent transitions the availability strip draws):

```sh
omniglass reachability list disp-1                              # needs component:read
```
`--image-base64` takes a plain base64 string, not a file path (base64-encode the image
yourself, as the `$(base64 …)` above does); the server accepts JPEG, PNG, or WebP and
normalizes it to a 256x256 JPEG. An administrator manages **any** principal's picture with
`omniglass principal setAvatar <id> --image-base64 …` and `omniglass principal removeAvatar <id>`
(gated by `principal:set-avatar`), reading one back with `omniglass avatar list <id>` (gated by
`principal:read`). A principal with no picture is a 404.

## Secrets

The [secret](/architecture/variables/) commands are generated like every other resource. `secret`
covers the encrypted values, `secret-type` lists the shape registry, and `effective-secret` reads the
masked cascade onto one component. Output is masked JSON, the same as the console; plaintext lives
behind `reveal`, which the server audits and which only admin and owner may call.

```sh
omniglass secret-type list                          # the shape registry (snmp-community, basic-auth)
omniglass secret list                               # the all-scope admin directory (masked fields)
omniglass secret create --name core-snmp --secret-type snmp-community \
  --owner-kind location --owner hq --fields '{"community":"public"}'
omniglass secret update <id> --fields '{"community":"s3cret"}'   # an omitted field keeps its value
omniglass secret reveal <id>                        # audited plaintext decrypt (secret:reveal)
omniglass secret delete <id>

omniglass effective-secret list <component>         # the masked cascade resolved onto a component
```

`--owner-kind` is one of `global | location | system | component`; `--owner` names the owning entity
and is omitted for a `global` secret (which needs an all-scope grant). Field maps pass as a JSON object
to `--fields`, validated against the type's shape.

## Variables

The [variable](/architecture/variables/) commands are generated the same way. `variable` covers the
plaintext values and `effective-variable` reads the cascade onto one component. There is no reveal:
the value is shown in the clear.

```sh
omniglass variable list                             # the all-scope admin directory
omniglass variable create --name poll_interval --value-type int \
  --owner-kind system --owner east-auditorium-av --value 30
omniglass variable create --name retry --value-type json --owner-kind global \
  --value '{"retries":3,"backoff":"1s"}'
omniglass variable update <id> --value 60           # validated against the fixed value_type
omniglass variable delete <id>

omniglass effective-variable list <component>       # the cascade resolved onto a component
```

`--value-type` is one of `string | int | float | bool | json`. `--value` is **parsed as JSON**, so a
bare `30`, `true`, or `{"k":"v"}` sends the number, the boolean, or the object; a bare word like `HDMI1`
falls back to a string, so the common case needs no quoting. A string value that would otherwise parse
as JSON (`30`, `true`) is quoted to force a string: `--value '"30"'`. (`secret create --fields` parses
the same way.)
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
