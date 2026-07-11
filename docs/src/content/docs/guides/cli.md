---
title: The CLI
description: "The omniglass CLI: a generated client of the HTTP API, with a stable seam for hand-written commands."
---

The `omniglass` binary is both the server and the client. Its data commands are
**generated from the OpenAPI** (`make gen`, via `cmd/cligen`), so the CLI cannot drift
from the API: a new route is a new command on the next regeneration. A small set of
commands (the run modes and the trusted bootstrap) are hand-written and compose with the
generated tree on the same root.

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
# lane, so it is exempt from the password policy (unlike the console/API paths).
omniglass bootstrap ops --password 'set-a-strong-one' --email ops@example.com --display-name "Ops Lead"

# Reprint a fresh bearer token for an existing user (direct-DB, owner lane). A CLI
# token does not expire (unlike a web-login session cookie, which has a fixed lifetime).
omniglass token ops

# Set or rotate a user's password (direct-DB, owner lane; also policy-exempt as the recovery path).
omniglass set-password ops 'set-a-strong-one'

# Seed a dev database with an example estate (locations, users, grants). Idempotent and
# dev-only; `make dev` runs it for you, so a fresh console is populated instead of empty.
# Never for production: these are operator rows, not ship-with reference data.
omniglass seed-dev
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
