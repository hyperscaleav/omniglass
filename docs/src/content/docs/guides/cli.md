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
owner whose token is printed once, and the server with the operator console at
`http://localhost:8080/web`. Ctrl-C stops the server; `make down` stops Postgres (the
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

The token is a bearer credential (mint the first one with `omniglass bootstrap`). The
server enforces the same capability and scope for the CLI as for any caller: the CLI is
just another client, with no privileged path.

```sh
export OMNIGLASS_SERVER=https://omniglass.example.com
export OMNIGLASS_TOKEN=ogp_...
omniglass location list
omniglass location create --name hq --location-type campus
omniglass location get hq
```

Output is JSON. A non-2xx response prints the server's error body and exits non-zero, so
the CLI is safe in scripts.

## Generated versus hand-written

- **Generated** (`internal/cli/api_gen.go`, do not edit): one command per API operation.
  The resource and verb come from the AIP-style path (`POST /locations` is `location
  create`, `GET /locations/{name}` is `location get <name>`, a `:verb` custom method is
  `<resource> <verb> <id>`); path parameters are positional args, the request body is
  `--flags`, and `--help` plus the example come from the operation's summary and
  description.
- **Hand-written** (`internal/cli/api_hooks.go` and the run-mode files): the client
  runtime the generated tree calls, plus commands that are not API operations, the
  `server` and `migrate` run modes and `bootstrap` (the trusted direct-DB owner lane).

To add a hand-written command, write a `newXxxCmd()` returning a `*cobra.Command` and add
it in `newRoot`, exactly as `bootstrap` does. Regenerating the API commands never touches
it.
