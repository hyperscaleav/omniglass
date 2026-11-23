---
title: The CLI
description: "The omniglass CLI: a generated client of the HTTP API for scripting, how it connects and signs in, and the seam for hand-written commands."
sidebar:
  order: 2
  badge:
    text: Built
    variant: success
---

The `omniglass` binary is both the server and the client. Its data commands are
**generated from the OpenAPI** (`make gen`, via `cmd/cligen`), so the CLI cannot drift
from the API: a new route is a new command on the next regeneration. A small set of
commands (the run modes and the trusted bootstrap) are hand-written and compose with the
generated tree on the same root.

This page is the mental model and the setup. For the exhaustive, generated list of every
command, its flags, and an example, see the **[CLI reference](/reference/cli/)**.

## Running the full stack locally

`omniglass lab run` (or `make lab`, the short form from a checkout) brings up the whole stack
for a browser session on Docker: Postgres, the server with the operator console at
`http://127.0.0.1:8080/web`, and the edge outpost `edge-hq` running the simulator in its own
process, serving and collecting from the seeded dev fleet. The login is `dev` / `dev`. Ctrl-C
removes the containers and keeps the data for the next run; `--fresh` starts over from the seed.
The [lab guide](/developer/lab/) covers what runs, its safety rails and its flags. `make` itself
only builds (`make lab` is `make`, then `./bin/omniglass lab run`). Tests never touch the lab: they spin their own
ephemeral Postgres via testcontainers.

The example fleet deliberately types almost none of its own names: its devices and its
systems all ask the platform to name them, so what you sign in to is the name generator's
own output (`display-1` in three different rooms, `board-meeting` and `board-meeting-2` for the
two halves of one) rather than a list somebody wrote out. The rows that do carry a typed
name are the ones no rule could produce: every LOCATION, since no shipped location type
carries a name rule (a campus, a building, a floor and a room each have a name you hold
and the platform does not).

### A server on ports it picks

`omniglass server` serves the API on `OMNIGLASS_ADDR` (`:8080` by default) and hosts the
embedded bus outposts connect to on `OMNIGLASS_NATS_ADDR` (`127.0.0.1:4222`). Either address
may name port 0, which binds whatever free port the kernel hands out, so a second server on
the same host (a test run, another worktree's stack) never collides with the first. To learn
the ports it got, set `OMNIGLASS_READY_FILE` to a path: once both listeners are bound, the
server writes the addresses there as one JSON object, written whole or not at all. A file
already at that path (one a killed server left) is cleared first thing at boot, before
anything that can fail, and the server removes its own at a graceful shutdown (SIGINT or
SIGTERM), so a file you find there names a live server's ports.

```json
{"addr":"127.0.0.1:41234","nats_addr":"127.0.0.1:41236","nats_url":"nats://127.0.0.1:41236"}
```

`nats_url` is the bus URL the outpost-claim exchange hands each outpost: `OMNIGLASS_NATS_URL` when
that is set, otherwise the address the bus bound (read as loopback when it bound a wildcard
host such as `0.0.0.0`), so a bus on port 0 tells outposts the port it got. Read the ports from
the file rather than finding a free port and passing it in: a port that is free when you look
can be taken by another process before the server binds it. The end-to-end tests start every
server this way ([test-driven](/contributing/test-driven/#tiers)).

## Connecting

Every generated command is a client of a running server and takes two shared flags,
`--server` and `--token` (the reference's [global flags](/reference/cli/#global-flags) table
is generated from the binary). Each resolves in one order when the command runs: the flag
when you give it, else its environment variable (`OMNIGLASS_SERVER`, `OMNIGLASS_TOKEN`), else
the flag's built-in default, which the token does not have.

The token is a bearer credential (mint the first one with `omniglass bootstrap`; see
[Authentication](#authentication) below). The server enforces the same capability and scope
for the CLI as for any caller: the CLI is just another client, with no privileged path.

The environment is read at that moment, never copied into a flag's default, because a default
is what `--help`, shell completion and the CLI reference print. So every one of them names
`$OMNIGLASS_TOKEN` and never shows its value, and help output is safe to paste into a ticket or
a chat ([#979](https://github.com/hyperscaleav/omniglass/issues/979)). Errors hold the same line:
a server URL or a database DSN whose embedded credentials do not parse is reported by
name, with the parser's detail withheld (it can quote the password), so percent-encode any
reserved character in it. A server URL is refused the same way when such a password would be
misread rather than refused (an `@` past the host, a query or a fragment, or no `http://` or
`https://` before it): the error would otherwise print the password, and the request could go,
token and all, to a host named after the user. Prefer the variable to typing `--token` itself,
which leaves the secret in your shell history and the process list.

```sh
export OMNIGLASS_SERVER=https://omniglass.example.com
export OMNIGLASS_TOKEN=ogp_...
omniglass location list
omniglass location create --name hq --location-type campus
omniglass location get hq
omniglass location rename hq --name hq-west          # needs location:rename, not location:update
```

Output is JSON. A non-2xx response prints the server's error body and exits non-zero, so
the CLI is safe in scripts.

### A flag carries the schema's own type

The commands are generated from the [API](/reference/api/), so a request field's type is a fact the
spec already states and the flag carries it
([#711](https://github.com/hyperscaleav/omniglass/issues/711)). An `integer` field is an integer flag
and `--settle-window-seconds soon` is refused **by the shell**, before a request is issued, rather
than by the server's 422 after a round trip. A `boolean` field is a bool flag, which is the one shape
with a spelling to learn: write `--propagates=false`, since a bare `--propagates` means true and
`--propagates false` reads `false` as a positional argument.

A field with no scalar type of its own (an object, an array, or a free-shape value) takes **one
string flag parsed as JSON**, which is a deliberate choice rather than a leftover. A nested value has
no shell-native flag type, and the alternatives (repeated `key=value` pairs, or a flag per leaf) can
express neither an array member nor `null`, which is the value that clears a field named in an
`update_mask` ([ADR-0106](/architecture/decisions/#adr-0106-a-location-type-is-platform-owned-and-a-nullable-object-clears-under-the-mask)):

```sh
omniglass location-type create --name wing --label Wing \
  --name-rule '{"stem":"wing","bare_first":true}'   # an object, as JSON
omniglass location-type update wing --update-mask name_rule --name-rule null   # clears it
omniglass tag create --name rack-position --applies-to '["location"]'          # an array, as JSON
```

A bare word that is not valid JSON falls back to itself, so the common case needs no quoting
(`--value HDMI1`); a string that would parse as JSON is quoted to force a string (`--value '"30"'`).

A name is renameable, and a rename is its own command rather than a flag on `update`: `component`,
`system`, `location`, and `user-group` each carry one, and so do the catalogs `vendor`,
`driver`, `product`, `standard`, and `location-type`
([ADR-0555](/architecture/decisions/#adr-0555-a-registry-row-renames-through-its-own-rename-custom-method)),
each gated by `<resource>:rename`, which is granted separately because moving a name breaks the
references stored outside Omniglass (bookmarks, runbooks, integration config). Inside it nothing
breaks, since every reference holds the entity's `id`, which every write returns
([ADR-0076](/architecture/decisions/)). A taken name is a 409, an illegal or uuid-shaped one a 422,
and an official catalog row keeps the name it ships with (422):

```sh
omniglass vendor rename acme --name acme-av        # needs vendor:rename (admin)
```

### Every command takes a name, never only a uuid

Every resource you type the name of answers to it, so a script or a runbook never has to look a uuid
up first ([ADR-1133](/architecture/decisions/#adr-1133-every-operator-resource-answers-to-its-name-and-an-endpoint-is-its-component-and-its-name)).
The uuid each command prints keeps working as well, and it is what to store when you need a handle
that survives a rename:

```sh
omniglass component get 'boi.17c.415a.$comp.display-1'      # a dotted address (quote the $comp)
omniglass endpoint retry 'boi.17c.415a.$comp.display-1/https'   # <component>/<endpoint name>
omniglass endpoint rotateToken 'hq.$comp.service-1/webhook'      # a webhook endpoint's URL, minted anew
omniglass unclaimed-source list                             # senders posting to a token no endpoint holds
omniglass secret update demo-panel-login --fields '{"password":"rotated"}'   # its name
omniglass secret reveal '$platform/demo-panel-login'        # <owner>/<name> where the name is shared
omniglass file download codec-firmware-2.1.4.txt            # a filename
omniglass user-group member create night-shift --user-id alice
omniglass user grant create alice --role viewer --scope-kind location --scope-id boi.17c
```

A name only you can see resolves for you even where someone else's out-of-scope row shares it, and
a name more than one row you can see answers to is a 404 listing each one in the spelling that
resolves, so the error tells you what to type next:

```text
secret "demo-panel-login" not found: 2 you can see answer to it; address one as $platform/demo-panel-login or boi.17c/demo-panel-login
```

A name is a lookup, so it lands on whatever the one row you can see by that name is now: a write
answers with the row it reached (a secret's `owner_kind` and `owner_name`), and a script that must
never drift, a credential rotation above all, addresses `<owner>/<name>` or stores the uuid.

### An unknown verb is a failure

A group (`component`, `component alarm`, `sim`) only routes to its subcommands. Run bare it prints
its help and exits 0; given a word it does not know, it exits non-zero with
`unknown command "raise" for "omniglass component alarm"` on stderr, so a script that guesses a verb
fails instead of passing green while nothing happened.

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
omniglass bootstrap ops --password 'set-a-strong-one' --email ops@example.com --label "Ops Lead"

# Mint a fresh bearer token for an existing user (direct-DB, owner lane). A --description
# (required) names what the token is for. Every credential is time-bounded: the token expires
# after --ttl (default 90 days, hard maximum 365 days; a --ttl above the cap is an error). A
# web-login session cookie has its own, shorter fixed lifetime.
omniglass token ops --description 'ci pipeline'
omniglass token ops --description 'nightly backup' --ttl 720h   # a 30-day token

# A signed-in user can mint its own token over the API (the console Create token action), which
# returns the secret once: omniglass auth create-token --description 'my laptop cli'.
# An AGENT token (--agent NAME, --ttl-days required, optional --roles and --channels as JSON
# arrays) acts on your behalf and can never reveal a credential; see /developer/agents/. The
# owner-lane `omniglass token` above does not mint agent tokens.
# An MCP client that signs you in with OAuth gets one too: `omniglass auth oauth-client get <id>`
# reads its request and `authorize <id> --approved --roles '["viewer"]' ...` answers it from the
# terminal, the same route the console's Connect page calls.

# Set or rotate a user's password (direct-DB, owner lane; also policy-exempt as the recovery path).
# A break-glass reset also revokes the user's live SESSIONS, so a stolen login stops at once; API
# tokens are kept unless --revoke-tokens is given (a full lockout of a compromised account).
omniglass set-password ops 'set-a-strong-one'
omniglass set-password ops 'set-a-strong-one' --revoke-tokens   # full lockout: sessions and tokens
```

Once a server is running, a signed-in user manages **its own** account through the
generated self-scoped commands (each edits only the caller's own profile):

```sh
omniglass auth me                                    # your user, permissions, and grants
omniglass auth agent-policy                          # what an agent may do, and whether it governs you
omniglass auth update-profile --label "Ops Lead"
omniglass auth change-password --current-password 'orange-boat-42x' --new-password 'purple-canyon-7'
omniglass auth set-avatar --image-base64 "$(base64 -w0 me.jpg)"   # set your profile picture
omniglass auth remove-avatar                            # clear it, falling back to initials
omniglass auth avatar                                   # read your picture back as { image_base64 }
```

`--image-base64` takes a plain base64 string, not a file path (base64-encode the image
yourself, as the `$(base64 …)` above does); the server accepts JPEG, PNG, or WebP and
normalizes it to a 256x256 JPEG. An administrator manages **any** user's picture with
`omniglass user setAvatar alice --image-base64 …` and `omniglass user removeAvatar alice`
(gated by `user:set-avatar`), reading one back with `omniglass user avatar list alice` (gated by
`user:read:admin`). A user with no picture is a 404.

## Importing a plan

`omniglass plan apply` lands a whole plan of locations, systems and components in one write
(`POST /plans:apply`, the shape and its rules in
[templates](/architecture/templates/#a-client-applies-a-plan-directly)). Each part of the plan is
a JSON flag, so a plan kept in a file goes in with `jq`; dry-run it first, then apply:

```sh
omniglass plan apply --validate-only \
  --standards "$(jq -c .standards plan.json)" \
  --locations "$(jq -c .locations plan.json)" \
  --systems "$(jq -c .systems plan.json)"
# the same flags without --validate-only apply it, and print the import_id
```

The dry run writes nothing and prints the counts and the names the apply would mint, or the
refusal (its path in the plan, `systems[3].components[1]`, leads the message). An apply prints the
`import_id` every audit row it wrote carries as `from_import`. A plan holds at most 1000 rows; split
a bigger import into one plan per building and apply each in turn.

## Everything else is in the reference

The commands for the rest of the API (collection, secrets, variables, tags, the catalogs, commands,
notifications, retention, platform health) follow the same grammar as the ones above: a resource, a
verb, the path parameters as arguments and the request body as flags. Each one, with its flags and an
example, is in the generated [CLI reference](/reference/cli/), which cannot drift from the binary; a
page here restating them would.

## Generated versus hand-written

- **Generated** (`internal/cli/api_gen.go`, do not edit): one command per API operation.
  The resource and verb come from the AIP-style path (`POST /locations` is `location
  create`, `GET /locations/{name}` is `location get <name>`, a `:verb` custom method is
  `<resource> <verb> <id>`, so the user lifecycle is `user disable <id>`,
  `user archive <id>`, `user restore <id>`, and `user purge <id>`); path
  parameters are positional args, the request body is
  `--flags`, and OpenAPI query parameters become optional `--flags` (a set flag is
  appended to the request query string, an unset one keeps the server default), so
  `user list --include-archived --kind machine` filters the listing. A user
  `<id>` argument accepts either the uuid or a person's username (`omniglass user
  archive alice`), resolved by the server, so you rarely need to look a uuid up first.
  `--help` plus the example come from the operation's summary and description.
- **Hand-written** (`internal/cli/api_hooks.go` and the run-mode files): the client
  runtime the generated tree calls, plus commands that are not API operations, the
  `server`, `outpost run`, and `migrate` run modes, the trusted direct-DB owner lane
  (`bootstrap`, `token`, `set-password`), the idempotent dev-fleet seeder
  (`seed-dev`, the same trusted lane, never for production), and `mcp`, which serves the
  [MCP tools](/reference/mcp/) over stdio for a local agent, with `mcp tools` and `mcp call`
  as a small client of it ([connect an agent](/developer/agents/)), and `omnibot`, the assistant
  that runs on the server's model and asks you on the terminal before any write (`--replay` runs it
  here on a recorded model instead)
  ([omnibot](/developer/agents/#omnibot-the-assistant-in-the-cli)), and with `--resume <name>` carries on
  a conversation the generated `assistant conversation` commands list and delete
  ([carrying a conversation on](/developer/agents/#carrying-a-conversation-on)); the generated
  `assistant conversation run` runs the same server run and prints its raw AG-UI event stream
  ([running omnibot on the server](/developer/agents/#running-omnibot-on-the-server)). The MCP tools are generated
  too, by `cmd/mcpgen` from the same OpenAPI document.

To add a hand-written command, write a `newXxxCmd()` returning a `*cobra.Command` and add
it in `newRoot`, exactly as `bootstrap` does. Regenerating the API commands never touches
it.
