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

`omniglass lab run` (or `make lab`, the short form from a checkout) brings up the whole stack
for a browser session on Docker: Postgres, the server with the operator console at
`http://127.0.0.1:8080/web`, and the edge node `edge-hq` running the simulator in its own
process, serving and collecting from the seeded dev fleet. The login is `dev` / `dev`. Ctrl-C
removes the containers and keeps the data for the next run; `--fresh` starts over from the seed.
The [lab guide](/guides/lab/) covers what runs, its safety rails and its flags. `make` itself
only builds (`make lab` is `make`, then `./bin/omniglass lab run`). Tests never touch the lab: they spin their own
ephemeral Postgres via testcontainers.

The example fleet deliberately types almost none of its own names: its devices and its
systems all ask the platform to name them, so what you sign in to is the name generator's
own output (`display-1` in three different rooms, `boardroom` and `boardroom-2` for the
two halves of one) rather than a list somebody wrote out. The rows that do carry a typed
name are the ones no rule could produce: every LOCATION, since no shipped location type
carries a name rule (a campus, a building, a floor and a room each have a name you hold
and the platform does not).

### A server on ports it picks

`omniglass server` serves the API on `OMNIGLASS_ADDR` (`:8080` by default) and hosts the
embedded bus nodes connect to on `OMNIGLASS_NATS_ADDR` (`127.0.0.1:4222`). Either address
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

`nats_url` is the bus URL the node-claim exchange hands each node: `OMNIGLASS_NATS_URL` when
that is set, otherwise the address the bus bound (read as loopback when it bound a wildcard
host such as `0.0.0.0`), so a bus on port 0 tells nodes the port it got. Read the ports from
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
a server URL or a database DSN carrying a `user:password` that does not parse is reported by
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
`system`, `location`, and `principal-group` each carry one, and so do the registries `vendor`,
`driver`, `product`, `standard`, and `location-type`
([ADR-0555](/architecture/decisions/#adr-0555-a-registry-row-renames-through-its-own-rename-custom-method)),
each gated by `<resource>:rename`, which is granted separately because moving a name breaks the
references stored outside Omniglass (bookmarks, runbooks, integration config). Inside it nothing
breaks, since every reference holds the entity's `id`, which every write returns
([ADR-0076](/architecture/decisions/)). A taken name is a 409, an illegal or uuid-shaped one a 422,
and an official registry row keeps the name it ships with (422):

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
omniglass principal-group member create night-shift --principal-id alice
omniglass principal grant create alice --role viewer --scope-kind location --scope-id boi.17c
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
omniglass auth update-profile --label "Ops Lead"
omniglass auth change-password --current-password 'orange-boat-42x' --new-password 'purple-canyon-7'
omniglass auth set-avatar --image-base64 "$(base64 -w0 me.jpg)"   # set your profile picture
omniglass auth remove-avatar                            # clear it, falling back to initials
omniglass auth avatar                                   # read your picture back as { image_base64 }
```

`--image-base64` takes a plain base64 string, not a file path (base64-encode the image
yourself, as the `$(base64 …)` above does); the server accepts JPEG, PNG, or WebP and
normalizes it to a 256x256 JPEG. An administrator manages **any** principal's picture with
`omniglass principal setAvatar alice --image-base64 …` and `omniglass principal removeAvatar alice`
(gated by `principal:set-avatar`), reading one back with `omniglass principal avatar list alice` (gated by
`principal:read:admin`). A principal with no picture is a 404.

## Collection commands

The [collection](/architecture/collection/) surface regenerates into three top-level command
groups, `node`, `endpoint`, and `task`; the composed reachability read hangs off the component
that owns it (`component reachability`). They follow the same derivation as every
other resource (`POST /endpoints` is `endpoint create`, `GET /endpoints/{endpoint}` is `endpoint get
<endpoint>`), so nothing here is special-cased. All examples require the matching permission on
the running server.

Check a driver spec, preview its polls through a node (storing nothing), and run an attached
endpoint's poll now (landing it), each covered in the [drivers guide](/guides/admin/drivers/#check-preview-and-run-a-poll-now):

```sh
omniglass driver validate --spec "$(cat spec.json)"
omniglass driver preview --node edge-hq --driver boreal-edge-web --inputs '{"host":"10.20.4.31","login":"display-login"}'
omniglass endpoint poll run 'hq.west.level-2.huddle.$comp.display-1/http' identity
```

Register and enroll an edge node (the day-one handshake):

```sh
omniglass node list
omniglass node create --name edge-hq --label "HQ Edge Node" --location hq-west --description "HQ network closet"   # needs node:create (all-scope)
omniglass node get edge-hq
omniglass node update edge-hq --label "HQ Edge" --location hq-west   # needs node:update; the name is immutable
omniglass node delete edge-hq   # needs node:delete; decommissions the node (cascades its endpoints, tasks, and enrollment)
```

Every command above is new in practice, not only in the docs: a hand-written `node` run
mode occupied the same name as the generated `node` group, so cobra resolved `omniglass
node list` to the daemon and it failed asking for `--token`. The run mode is now
`omniglass node run`, a verb beside the others:

```sh
omniglass node run --name edge-hq --token ogp_...   # the edge daemon: claim, pull the worklist, heartbeat
```

The [simulator](/architecture/simulation/) runs beside a node with that node's identity, serving the
devices placed on it, and the node's simulation view shows what it serves and what reached it:

```sh
omniglass sim run --server https://omniglass.example --name edge-hq --token ogp_... --port-offset 10000   # serve edge-hq's simulated devices
omniglass node simulation edge-hq   # connected?, capabilities, and hits per simulated device
omniglass component impairment set panel-1 --unplugged       # break it: dark on every protocol
omniglass component impairment set panel-1 --latency-ms 300  # or slow it
omniglass component impairment set panel-1                   # repair it
omniglass simulation get                                     # every simulator, its devices and their hits
omniglass simulation profiles                                # the profile catalog and the products each simulates
omniglass component simulation get panel-1                   # its API surface: protocols, routes, commands, OIDs
omniglass component simulation fire panel-1 --surface nvp --command "SET INPUT hdmi2"   # the device answers
omniglass component event list panel-1                       # what it said: its input-changed event, among others
omniglass component log list panel-1 --severity warning      # its log lines at warning or worse (RFC 5424's levels)
omniglass component log list panel-1 --source tcp            # only what arrived on one channel; the answer lists them all
omniglass simulation add-component edge-hq --name panel-9 --product newtron-panel-7       # served on the next pull
omniglass simulation plan-system meeting-room                                               # each role and the product it would get
omniglass simulation add-system edge-hq --standard meeting-room --system-type meeting --location boardroom-a   # the whole system
```

A set replaces the whole impairment, so name every fault you want at once: `--latency-ms 300` on an
unplugged device plugs it back in.

Impairments apply only on a simulator started with `--impairments` and `NET_ADMIN`, in a namespace
of its own, since it rewrites that namespace's firewall.

A node can also run the simulator in its own process: `omniglass node run --simulate` serves the
components placed on that node on the node's own loopback and collects from them, on one identity
and one enrollment. `--impairments`, `--port-offset` and `--poll` are the simulator's flags and are
refused without `--simulate`; `omniglass sim run` stays for a simulator as a process of its own.

```sh
omniglass node run --name edge-hq --token ogp_... --simulate                # the node and its simulated components, one process
omniglass node run --name edge-hq --token ogp_... --simulate --impairments  # in a container of its own, with NET_ADMIN
```

The [lab](/guides/lab/) runs exactly that, with `--impairments`, in a container of its own, so the
seeded fleet is collected for real and every impairment above works with nothing but Docker.

The node-facing `claim` exchange is public (the enrollment token is the authentication), so
a node presents its name and token to receive its NATS credential:

```sh
omniglass node claim --name edge-hq --token ogp_...
```

`omniglass node enroll <name>` mints (or re-mints) a node's enrollment token and prints it
once (gated by `node:enroll`), the same action as the console's Enroll / Re-enroll.

Author a reachability check by **creating an endpoint** (its poll task is derived
automatically):

```sh
# An endpoint owned by a component, placed on a node, with its probe target in params.
# It is named by its transport: --transport picks from the code registry, there is no --name flag.
omniglass endpoint create \
  --transport tcp --component disp-1 --node edge-hq \
  --params '{"target":"10.0.0.1:22"}'                          # needs endpoint:create

# Or ATTACH a driver instead of picking a transport: the driver's spec derives the
# transport, the target (from the host and port inputs), and the endpoint's tasks
# (a poll task per poll function, a standing listen task per listener). Secret
# inputs are references: the NAME of a secret of the declared shape, never a value.
omniglass endpoint create \
  --driver snmp-generic --component amp-1 --node edge-hq \
  --inputs '{"host":"10.0.0.40","community":"lab-community"}'  # needs endpoint:create

omniglass endpoint list
# An endpoint is <component>/<endpoint name>: its name is its transport, unique on its component,
# and the component is any spelling the component commands take (a name, a dotted address, a uuid).
omniglass endpoint get disp-1/tcp
omniglass endpoint update disp-1/tcp --node edge-hq --params '{"target":"10.0.0.2:22"}'
# A driver-attached endpoint's address and secret edit in place, through its inputs
# (validated against the driver; its tasks re-derive in the same transaction):
omniglass endpoint update amp-1/snmp --inputs '{"port":"2323","community":"lab-community-2"}'
omniglass endpoint delete disp-1/tcp                            # refused (409) while its task references it

# An endpoint a refusal gated (endpoint-rejection names the rung, endpoint-rejection-reason says why,
# held session or not) is not attempted again until what it connects with changes, or an operator
# retries it:
omniglass endpoint retry disp-1/ssh                             # needs endpoint:retry; audited
omniglass endpoint retry disp-1/ssh --host-key SHA256:...       # ssh: trust the offered key the refusal names (409 for any other)
omniglass endpoint retry disp-1/https --certificate SHA256:...  # tls or https: the same for a certificate's public key (--host-key works too)
# A tls or https endpoint's verification mode is one of its driver's inputs, edited in place:
omniglass endpoint update disp-1/https --inputs '{"tls-mode":"verify"}'  # verify, pin or skip

# The poll task is derived from the endpoint, so the task surface is read-only.
omniglass task list
omniglass task get <id>
```

The transports are a code registry the binary ships (`omniglass transport list` reads it):
`icmp` and `tcp` probe layers 3 and 4, `telnet` reads its first bytes, `tls` runs the handshake and reports the
certificate's expiry, `ssh`, `http` and `https` (the handshake, then a response) climb to layer 7
(the probe draws a real response, so reached-but-not-responsive and responded-but-not-authenticated
are visible states; an http probe sends no credential, so its driver's polls prove the login), `udp` proves its rung with its driver's request (a bare udp endpoint has nothing to send),
and `snmp` proves its rung with a standard get (sysUpTime.0) carrying its driver's community.
An endpoint is **named by its transport** (the `--transport`, or the attached driver's spec),
unique within its component. An endpoint `update` changes only its node placement and
params. A **task** is **derived** when its endpoint is created, so there is no
`task create`, `update`, or `delete`; its placement follows the endpoint's. A node purge
cascades its endpoints and their derived tasks.

Read a component's composed reachability (the verdict, the probe-layer signals, and the
recent transitions the availability strip draws):

```sh
omniglass component reachability list disp-1                              # needs component:read
```

Read where each of a component's properties stands with its property list: every property it
has (declared, calculated, observed or told) comes back as a row, and every row but a calculated
one carries its `reconciliation`, the want/told/is pivot (the current declared value with the
contract default coalesced in, the newest intended value a command set, and the observed value
read across every endpoint that reports it, naming the one it shows in `is_instance`, all series
reads) with drift computed on read. A property only observed reads `want: null` and never drifts.
A system's list carries the same block:

```sh
omniglass component property list disp-1 | jq -c '.properties[] | {property_type_name, reconciliation}'   # needs component:read
omniglass system property list boardroom                                     # needs system:read
```

Push telemetry an integration observed, for a component or for a system as a whole (a cloud
service's account of the room it serves). The owner's kind and name ride `--owner`; each lane is a
JSON array checked against its own catalog, and an unregistered name comes back under `rejected`.
A system's observations are owner-level (a system has no endpoints, so they name no instance), and a
system batch carries no log lines, since a log line is a component's:

```sh
omniglass telemetry push --owner '{"kind":"system","ref":"huddle-1"}' --source room-booking-cloud \
  --metrics '[{"name":"occupancy-count","value":4}]' \
  --properties '[{"name":"video-input","value":"hdmi2"}]'                          # needs telemetry:push
```

A system's samples read on its own surfaces: its metrics (an observed or a calculated series), its
properties (declared, a calc rule's, or observed by a push, each row carrying its reconciliation, so a
pushed property nothing declares reads `want: null`, its `is`, and no drift), and one series' history:

```sh
omniglass system metric list huddle-1                                                  # needs system:read
omniglass system property list huddle-1 | jq -c '.properties[] | {property_type_name, reconciliation}'   # needs system:read
omniglass system property sample list huddle-1 video-input                             # needs system:read
```

## Secrets

The [secret](/architecture/variables/) commands are generated like every other resource. `secret`
covers the encrypted values, `secret-type` lists the shape registry, and `component
effective-secret` reads the masked cascade onto one component. Output is masked JSON, the same as the console; plaintext lives
behind `reveal`, which the server audits and which only admin and owner may call.

```sh
omniglass secret-type list                           # the shape registry (snmp-community, basic-auth)
omniglass secret list                               # the all-scope admin directory (masked fields)
omniglass secret create --name core-snmp --secret-type snmp-community \
  --owner-kind location --owner hq --fields '{"community":"public"}'
omniglass secret update core-snmp --fields '{"community":"s3cret"}'   # an omitted field keeps its value
omniglass secret reveal hq/core-snmp                # audited plaintext decrypt (secret:reveal), by <owner>/<name>
omniglass secret delete core-snmp

omniglass component effective-secret list codec-1   # the masked cascade resolved onto a component
```

The effective read answers **which** secret applies to a device and where it comes from, never
what it contains: fields are masked exactly as the directory masks them, and plaintext is only
ever `secret reveal`. It rides `secret:read`, which the viewer floor does not carry.

`--owner-kind` is one of `platform | location | system | component`; `--owner` names the owning entity
and is omitted for a `platform` secret (the install-wide tier, which needs an all-scope grant plus
`platform:create`). Field maps pass as a JSON object to `--fields`, validated against the type's shape.

A secret's name is unique within its owner, and the cascade makes one name at several owners the
ordinary case (a room's `core-snmp` overriding the platform's), so the bare name resolves only where
exactly one secret you can see carries it. `<owner>/<name>` always resolves: the owner by name,
dotted address or uuid, or `$platform` for the install-wide tier, which has no name of its own
(`omniglass secret reveal '$platform/core-snmp'`).

## Variables

The [variable](/architecture/variables/) commands are generated the same way. `variable` covers the
plaintext values and `component effective-variable` reads the cascade onto one component. There is
no reveal: the value is shown in the clear.

```sh
omniglass variable list                             # the all-scope admin directory
omniglass variable create --name poll-interval --value-type int \
  --owner-kind system --owner east-auditorium-av --value 30
omniglass variable create --name retry --value-type json --owner-kind platform \
  --value '{"retries":3,"backoff":"1s"}'
omniglass variable update east-auditorium-av/poll-interval --value 60   # validated against the fixed value_type
omniglass variable delete retry

omniglass component effective-variable list codec-1   # the cascade resolved onto a component
```

Both effective reads return the **winner and the candidates it beat**, with the tier each came
from, so the answer explains itself rather than just asserting a value. The variable cascade
resolves the system band from the component's **primary** membership; resolving against a named
system is offered only by `component effective-tag list` today.

`--value-type` is one of `string | int | float | bool | json`. `--value` is **parsed as JSON**, so a
bare `30`, `true`, or `{"k":"v"}` sends the number, the boolean, or the object; a bare word like `HDMI1`
falls back to a string, so the common case needs no quoting. A string value that would otherwise parse
as JSON (`30`, `true`) is quoted to force a string: `--value '"30"'`. (`secret create --fields` parses
the same way.)

A signed-in principal manages its own account through the generated self-scoped commands
(`omniglass auth me`, `auth update-profile`, `auth change-password`, `auth set-avatar` /
`auth remove-avatar`); an administrator manages other principals, roles, groups, secrets,
and variables through the resource commands, all covered in the [admin guide](/guides/admin/)
and listed in full in the [CLI reference](/reference/cli/).

## Tags

The [tag](/architecture/tags/) commands split along the governance line. The `tag` resource covers the
**key vocabulary** (minting, editing, and deleting keys, plus the install-wide `platform` binding), while binding a
value onto an entity is a **custom method on the entity** (`component setTag` and friends), so it needs
only the write the operator already holds on that entity. `effective-tag` reads the resolved cascade onto
one component.

```sh
omniglass tag list                                  # the governed key vocabulary
omniglass tag create --name environment             # mint a key (tag:create, admin)
omniglass tag create --name rack-position --applies-to '["location"]' --propagates=false
omniglass tag update environment --applies-to '["component","system"]'
omniglass tag setPlatform environment --value prod  # an install-wide default (tag:update + platform:update)
omniglass tag clearPlatform environment
omniglass tag delete environment                    # cascades its bindings

omniglass component setTag codec-1 --key environment --value dev    # component:update
omniglass component listTags codec-1                # the bindings set directly on the component
omniglass component removeTag codec-1 --key environment
omniglass system setTag east-auditorium-av --key environment --value prod
omniglass location setTag hq --key environment --value staging

omniglass component effective-tag list codec-1                # the cascade resolved onto a component
```

Binding is a custom method on the entity (`component setTag`), like the principal lifecycle verbs, so it
stays clear of the top-level `tag` commands. A tag's `--name` is an ordinary entity name (lowercase
letters, digits, and hyphens, no dots and no underscores, never a uuid), and minting one that breaks
the rule is a 422; that name is then the key a binding carries (`--key`). `--applies-to` is an
entity-kind allow-list passed as a JSON array (`'["component","system"]'`; empty means universal),
checked when a value is bound. `--propagates`
defaults true (the value cascades to descendants); `--propagates=false` binds a flat per-entity value
that resolves only on its own entity. Resolving onto a component **unions** keys and **overrides** values
most-specific-wins down the cascade.

## Component classification catalogs

The [component-classification catalog](/architecture/core-entities/#catalog-reference-data-vendor-driver-component_type)
commands cover the `vendor` and `driver` registries: flat, official-vs-custom catalogs on
the same pattern as the `type` registries. Each resource's `:read` sits on the viewer floor; the four
writes (`:create`, `:update`, `:delete`, `:rename`) are admin-gated.

A **vendor** names an organization, carrying a `--kind` of `manufacturer`, `integrator`, or `developer`
(default `manufacturer`):

```sh
omniglass vendor list                                               # the vendor registry
omniglass vendor create --name barco --label Barco --kind manufacturer \
  --icon monitor --support-phone "+1-555-0100" --website https://www.barco.com
omniglass vendor get barco
omniglass vendor update barco --support-phone "+1-555-0199"
omniglass vendor rename barco --name barco-av                       # every product that names it follows
omniglass vendor delete barco                                       # refused (422) if official
```

A **driver** names the implementation that gets, emits, or sets a product's signals, with an optional
`--version`:

```sh
omniglass driver list                                               # the driver registry
omniglass driver get snmp-generic                                   # includes the spec body, when authored
omniglass driver create --name barco-snmp --label "Barco SNMP" --version 1.0.0 \
  --spec '{"version":1,"transport":"snmp", ...}'                    # validated at write: a broken spec is a 422 naming the fault
omniglass driver update barco-snmp --version 1.1.0
omniglass driver rename barco-snmp --name barco-projector-snmp      # re-derives the tasks of every endpoint attached through it
omniglass driver delete barco-snmp                                  # refused (422) if official, or while an endpoint is attached through it
```

An **official** row, for example the `crestron` vendor, is read-only: `update`, `delete` and `rename`
all 422. A vendor's `website` is validated to an `http`/`https` scheme on write; any other scheme (for
example `javascript:`) is a 422.

## Products

The [product](/architecture/core-entities/#catalog-reference-data-product) commands cover the product
registry: the concrete **SKU** that ties the vendor and driver catalogs together, classified under a
**component_type**, and the thing a `component` points at. `product:read` sits on the viewer floor; the
four writes (`product:create`, `product:update`, `product:delete`, `product:rename`) are admin-gated.

A product names its **kind** (`device`, `app`, `service`, or `vm`, default `device`), the
**component_type** it is classified under, and optionally its **vendor**, **driver**, and a
**parent product** it is a variant of:

```sh
omniglass product list                                              # the product registry
omniglass product create --name barco-ub12 --label "Barco UB12" --kind device \
  --component-type projector --vendor-id barco --driver-id barco-snmp
omniglass product get barco-ub12
omniglass product update barco-ub12 --label "Barco UB12 Pro"
omniglass product delete barco-ub12                                 # 422 if official, 409 if a component points at it
```

An **official** product, for example `cisco-room-bar`, is read-only: `update`, `delete` and
`rename` all 422. A product still referenced by a **component** (`component.product_id`) cannot be deleted (409);
an unknown vendor, driver, parent, or component_type id is a 422.

## Standards

The [standard](/architecture/core-entities/#catalog-reference-data-standard) commands cover the
system-side counterpart of a product: the **blueprint a system conforms to**. `standard:read` sits on the
viewer floor; `standard:create`, `standard:update`, `standard:delete`, and `standard:rename` are
admin-gated.

```sh
omniglass standard list                                             # the standard catalog
omniglass standard create --name lecture-hall --label "Lecture Hall" \
  --parent-standard-id classroom                                    # a variant of an existing standard
omniglass standard get lecture-hall
omniglass standard delete lecture-hall                              # 409 if a system still conforms to it
```

A fresh install holds **no standards**: start one from a template in the catalog the binary ships, or
create one from nothing as above. `omniglass template list --kind standard_template` lists the
templates, and creating from one writes a standard of your own (`official: false`) with the template's roles,
choices and contract lines:

```sh
omniglass standard createFromTemplate --template meeting-room                     # takes the template's name
omniglass standard createFromTemplate --template meeting-room --name hq-meeting   # a second, independent standard
omniglass standard createFromTemplate --template mr55 --parent hq-meeting         # a variant, under a parent you hold
```

A taken name is a 409, and a variant whose parent you do not hold is a 422 naming the template to
create from first. Nothing points back at the template, so the standard is yours to rename, edit and delete. See
[the seed model](/architecture/core-entities/#the-seed-model-templates-versus-canonical-catalogs).

### Create fleet entities from a template

A component, a whole system, or a subtree of places is created from a template the same way, with the
same `createFromTemplate` command: name the
template and where it lands. Everything lands in one write or not at all, and nothing points back at the
template. `inputs` and `attach` take JSON:

```sh
omniglass component createFromTemplate --template newtron-panel-7 --location hq \
  --node edge-hq --inputs '{"host":"10.20.0.7"}'                     # attached; without inputs it lands unattached
omniglass system createFromTemplate --template mr55-system --location hq.f3.mr-1 \
  --node edge-hq --attach '{"touch-control":[{"host":"10.20.0.8"}]}' # creates mr55 (and meeting-room) from their templates if you hold neither
omniglass location createFromTemplate --template meeting-floor --parent hq --name f4
```

A system's answer lists `to_attach`, the components that landed with no endpoint: attach each later
with `omniglass endpoint create`. A subtree whose root type its parent refuses is a 422 naming the pair.

## Event types

The [event type](/architecture/events/#the-event_type-registry) commands cover the occurrence keyspace,
the twin of the property catalog: the discrete happenings an event is typed by (`call-started`,
`cable-unplugged`). `event_type:read` sits on the viewer floor; `event_type:create`, `event_type:update`, and
`event_type:delete` are admin-gated. Official event types are read-only.

```sh
omniglass event-type list                                           # the event type catalog
omniglass event-type create --name cable-unplugged --label "Cable unplugged"
omniglass event-type get call-started
omniglass event-type delete cable-unplugged                        # 409 on an official type
```

Every metric, property and event type reads back its `writer_class` (`node-probe`, `platform`,
`command-lifecycle`, `calc`, `collected`): who may write the key ([who writes a key](/architecture/properties/#who-writes-a-key-the-writer-class)).
`omniglass event-type get collection-failed` reads `node-probe`, the node's own account, and
`command-issued` reads `command-lifecycle`, a command's alone; a create under a reserved name is a 422
on every catalog.

## Command types

The [command type](/architecture/commands/#the-command_type-registry) commands cover the "do"
catalog: what a component can be told, with a target property and a settle window. `command_type:read`
sits on the viewer floor; create/update/delete are admin-gated, official types read-only. Issuing a
command (`command:issue`) records the invocation, writes a caused event, and returns the computed
settlement verdict; the invocation then rides the per-node command queue to the placed node,
which actuates it through the attached driver's command binding, and the settle loop closes
against the observed value the driver's polls report back
([commands](/architecture/commands/#actuation-the-wire-to-the-device)).

```sh
omniglass command-type list                                         # the command type catalog
omniglass command-type create --name set-input --target-property-type video-input --settle-window-seconds 15
omniglass command-type get reboot
omniglass component command issue disp-1 --command-type set-input --value '"hdmi2"'   # needs command:issue
```

A target must be a key a command may write: a native probe key, `health` or a calc rule's output is
refused (422) naming the key.

### A command's status, from issue to settlement

The issue answers with the command as it stands: its `status`, its `phase` (where it sits while in
flight: `queued` for its node, `delivered`, `awaiting-reply` or `awaiting-observation`), and the
evidence once there is some. `--wait` holds the issue until the command settles, fails or times out,
or until `--wait-timeout` seconds pass (1 to 120, 30 by default), which is what a script wants; a
server holds 64 waits at once and a principal 4, and a wait past either is a 429 refused before
anything is issued. Every command reads back by its id under its component, with `command:read`
([commands](/architecture/commands/#reading-a-command-from-issue-to-settlement)):

```sh
bar='depot.bay-1.$comp.videobar-1'
omniglass component command issue "$bar" --command-type set-volume --value 42 --wait \
  | jq -c '{status, settled_by, reason, reply}'
# {"status":"settled","settled_by":"reply","reason":"the reply confirmed it","reply":"{\"ok\": true}\n"}
omniglass component command list "$bar" --status timed-out --since 2026-10-25T09:00:00Z
omniglass component command get "$bar" 42 | jq -c '{status, phase, reason, endpoint, node, driver}'
```

The `reply` is the component's answer as its node reported it, masked of the endpoint's credentials
and of anything labeled a credential before it was stored; a timed-out command's `reason` says where
it stopped (`queued for node edge-depot-dark, which did not take it within its 15s settle window`).

## Notifications

The [notification](/architecture/alarms-actions/#notifications) commands send an alarm to the chat or
paging tool somebody watches: a `notification-type` says what a message says (an operator writes one,
`preview` renders it against a real alarm), a `notification-target` says where it goes (admin-only,
its URL masked until `reveal`), an `action-rule` says when (`rule:*`, all-scope), and a
`notification` is one message on its way. The whole task is the
[notifications guide](/guides/admin/notifications/).

```sh
omniglass notification-type preview alarm-raised --component panel-2 | jq -c '{subject, text}'
omniglass notification-target create --name ops-chat --kind webhook --url https://chat.example.com/hooks/ops --secret ops-chat-signing
omniglass notification-target test ops-chat                           # one synthetic send: {ok, status}
omniglass action-rule create --name page-ops --on '["raised","cleared"]' --when 'severity_rank >= 40' --targets '["ops-chat"]'
omniglass notification list --status failed                          # scoped by the alarm's component
omniglass notification retry <id>                                     # notification:retry, an operator's
```

## Property contracts and values

A classifier **declares** which properties its instances carry; an instance **sets** a value. Both sides
are the same three verbs, and the contract commands hang off the classifier that owns them:

```sh
omniglass product property list cisco-room-bar                         # a product's contract
omniglass standard property list huddle-room                           # a standard's contract
omniglass location-type property list room                             # a location type's contract
omniglass standard property update huddle-room room-capacity --default-value 6 --required=true
omniglass standard property delete huddle-room room-capacity        # systems keep any value they set

omniglass component property list dsp-boardroom-3                      # every property, reconciled
omniglass system property list boardroom                               # same shape, system side
omniglass location property list east-campus                           # same shape, location side
omniglass system property update boardroom room-capacity --value 12    # idempotent
omniglass system property delete boardroom room-capacity             # falls back to the contract default
```

The read resolves the classifier's contract against the instance's own values, so a **one-off system**
(one conforming to no standard) and a **productless component** still resolve, to their off-contract
values alone. The value commands are **scope-injected**: an instance outside your read scope is a
non-disclosing 404 on the read and on the write. The registry and its contract share one noun:
`omniglass location-type list` is the registry, `omniglass location-type property list <id>` its contract.

## Retention

How long the server keeps each lane is the `retention` settings: `omniglass setting list` shows the
effective windows under `values.retention`. They are changed on the console's Settings page; the
CLI's `setting update` cannot carry a body yet (#1333). A metric or property type can keep a window
of its own, overriding its lane's, shipped types included:

```sh
omniglass metric-type setRetention tcp-connect-time --days 3       # keep this type 3 days
omniglass property-type setRetention endpoint-reachable --days 365
omniglass metric-type setRetention tcp-connect-time --days null    # back to the lane's window
omniglass metric-type get tcp-connect-time | jq .retention_days
```

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
  `server`, `node run`, and `migrate` run modes, the trusted direct-DB owner lane
  (`bootstrap`, `token`, `set-password`), the idempotent dev-fleet seeder
  (`seed-dev`, the same trusted lane, never for production), and `mcp`, which serves the
  [MCP tools](/reference/mcp/) over stdio for a local agent, with `mcp tools` and `mcp call`
  as a small client of it ([connect an agent](/guides/agents/)). The MCP tools are generated
  too, by `cmd/mcpgen` from the same OpenAPI document.

To add a hand-written command, write a `newXxxCmd()` returning a `*cobra.Command` and add
it in `newRoot`, exactly as `bootstrap` does. Regenerating the API commands never touches
it.
