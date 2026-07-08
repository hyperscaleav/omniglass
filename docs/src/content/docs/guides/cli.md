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
# credential (argon2id) so the owner can sign in to the console.
omniglass bootstrap ops --password 's3cret-pw' --email ops@example.com --display-name "Ops Lead"

# Reprint a fresh bearer token for an existing user (direct-DB, owner lane).
omniglass token ops

# Set or rotate a user's password (direct-DB, owner lane).
omniglass set-password ops 'new-s3cret-pw'

# Seed a dev database with an example estate (locations, users, grants). Idempotent and
# dev-only; `make dev` runs it for you, so a fresh console is populated instead of empty.
# Never for production: these are operator rows, not ship-with reference data.
omniglass seed-dev
```

Once a server is running, a signed-in principal manages **its own** account through the
generated `auth` commands (self-scoped: each edits only the caller's own profile):

```sh
omniglass auth me                                    # your principal, permissions, and grants
omniglass auth update-profile --display-name "Ops Lead"
omniglass auth change-password --current-password 's3cret-pw' --new-password 'brand-new-pw'
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

## Generated versus hand-written

- **Generated** (`internal/cli/api_gen.go`, do not edit): one command per API operation.
  The resource and verb come from the AIP-style path (`POST /locations` is `location
  create`, `GET /locations/{name}` is `location get <name>`, a `:verb` custom method is
  `<resource> <verb> <id>`); path parameters are positional args, the request body is
  `--flags`, and `--help` plus the example come from the operation's summary and
  description.
- **Hand-written** (`internal/cli/api_hooks.go` and the run-mode files): the client
  runtime the generated tree calls, plus commands that are not API operations, the
  `server` and `migrate` run modes and the trusted direct-DB owner lane (`bootstrap`,
  `token`, `set-password`).

To add a hand-written command, write a `newXxxCmd()` returning a `*cobra.Command` and add
it in `newRoot`, exactly as `bootstrap` does. Regenerating the API commands never touches
it.
