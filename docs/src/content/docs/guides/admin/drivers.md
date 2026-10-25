---
title: Drivers
description: "The Drivers catalog: each driver's declarative spec (the menu one engine interprets), official rows read-only, admin-gated custom ones, and the attach flow that consumes them."
---

**Catalog, under Components: Drivers** (`/drivers`, with `driver:read`, covered by every viewer's `*:read` floor)
is the directory of **drivers**: the normalized menu that turns a device family's API into
what to fetch, how to parse it, and what actuates,
on the same flat-registry pattern as [Location Types](/guides/admin/location-types/) and [Tags](/guides/admin/tags/).
Where a [vendor](/guides/admin/vendors/) names who a device comes from, a driver names how it is
talked to (for example `Generic SNMP` or `Newtron NVP`). Each row shows the **name** (the
operator-facing name, for example `snmp-generic`), the **label**, an optional
**version**, and its **origin** (**official** or **custom**). A driver also carries
an `id`, a uuid minted by the database, the internal address the handle resolves to
([ADR-0062](/architecture/decisions/)); the handle is what you type and read.

A driver's body is its **spec** ([ADR-0135](/architecture/decisions/#adr-0135-a-drivers-spec-is-data-one-engine-interprets-it)):
a versioned declarative document (data, not code) naming the transport it rides, the **inputs**
an attach must supply (host, port, credentials as secret references), and its three function
families (poll functions, listeners, command bindings). The detail blade renders the spec as
the **menu it is**: what each poll asks and what lands, what each listener waits for, what each
command binding actuates. On a line protocol a listener's **arm** commands (the subscribe
conversation) are sent on every connection the node holds, its **match** rule claims the
component's pushes, and each claimed line lands as a sample, an event or a log line with no poll involved (a listener rides a
held line session or a udp socket, and is refused at write on any other transport, where nothing
would run it); a request may
declare its **reply** shape (`request.reply`, a `prefix` or `regex`) so its answer is picked out of
those pushes, plus an optional `error` shape (`reply.error`) for the device's refusals, which then
answer the request as a failure carrying the device's text instead of timing out. Every `regex` in
a spec is Go RE2 with **substring** semantics: `INPUT \S+` also matches `EVT INPUT hdmi2`, so anchor
a pattern meant to match a whole line (`^INPUT \S+$`). `unmatched: collection-failed` makes a line
nobody claims a collection fault, and `unmatched: log` a log line on the component, rather than a counted discard ([routing](/architecture/collection/#listeners-over-a-held-session)).
An emit names a **collected** key. A key only the node writes (every native probe key: the
reachability verdicts and probe timings, `tls-certificate`, `ssh-host-key`, `endpoint-rejection` and
its `endpoint-rejection-reason`, a held session's `session-state` and `session-reason`, and
`collection-failed`), `health`,
`command-issued`, or a key a calc rule outputs is refused at the spec's write, naming the key, since
each has its own writer ([who writes a key](/architecture/properties/#who-writes-a-key-the-writer-class)):
the node would otherwise take a driver's value as its own observation, the pin or the gate. A spec
stored before that refusal is held to it where it acts: the worklist strips such an emit from the
tasks a node receives (the rest still run), a re-derivation never bakes one, and the server names the
spec and its tasks in a warning at boot so you can mend it.

An emit lands on **any of the four lanes** ([an emit lands on any lane](/architecture/collection/#an-emit-lands-on-any-lane)).
A metric or a property names its key and one `extract`. An **event** names its event type and
carries a `message` and `attributes`, each a constant (`value`) or an `extract` with an optional
`transform` (a cast to `int` or `float` renders a number in the payload); the message defaults to
the whole claimed line or answer, and attributes the event type's `payload_schema` closes out, or
leaves out a field it requires, are refused at write. A **log line** declares `log` instead of a
name, with its `severity` and `facility` (each a constant or an extract), its `source` (by default
the endpoint's transport), a `message` and `attributes`; its severity maps onto RFC 5424's eight
levels (`warn` reads `warning`, `err` reads `error`, and a word the table does not know is kept as
the `severity_raw` attribute). A log line always lands: a message that does not extract falls back
to the whole line, and a severity or attribute that does not is left out, each a
`collection-failed` naming it. Any emit may read its time from what it reads (`ts`, an `extract`
and a `layout`: `rfc3339`, `rfc3164` with an optional `zone`, `unix`, `unix-ms`, or `go:` a Go
reference layout); a time more than five minutes ahead lands at arrival. On a plain poll an event or
a log line lands when its content changes, not every run. `unmatched: log` lands each line no
listener claims as a log line, whole. The shipped `newtron-nvp` lands an input push as the
`video-input` property and an `input-changed` event, the panel's diagnostics lines as log lines
with their severity, and anything else it pushes as raw log lines:

```yaml
unmatched: log
listeners:
  - name: events
    arm: ["SUBSCRIBE INPUT", "SUBSCRIBE SCENE"]
    match: { prefix: "EVT INPUT " }
    emits:
      - { name: video-input, extract: { regex: "^EVT INPUT (\\S+)$" } }
      - name: input-changed
        message: { value: "Video input changed" }
        attributes:
          input: { extract: { regex: "^EVT INPUT (\\S+)$" } }
  - name: logs
    arm: ["SUBSCRIBE LOG"]
    match: { prefix: "LOG " }
    emits:
      - log:
          severity: { extract: { regex: "^LOG (\\S+)" } }
          facility: { value: panel }
        message: { extract: { regex: "^LOG (?:\\S+\\s+)?(.+)$" } }
```
A spec may declare a **session** block, the auth step the held session runs before anything rides
it ([auth and rejection](/architecture/collection/#auth-and-rejection)). `session.login` is a login
dialog: its `steps` each wait for a prompt (`expect`, a `prefix` or `regex` that may match a prompt
with no line ending, such as `login: `) and `send` a line, which may name a secret input's field
(`${secret.login.password}`, the input declared `kind: secret`), and its `ready` match says the
login passed. `session.terminal` lists the replies retrying cannot fix, each a `match` with a
`class` (`auth`, `host-key`, `certificate`, `protocol`): a line matching one, during the login or
answering any request, moves the session to `rejected`, where the node attempts nothing more until
the endpoint's connection fingerprint changes (its secret is rotated, its address, port or transport
changes, the driver's login declaration changes) or an operator retries it (**Retry** on the
rejected session's notice in the console, or `omniglass endpoint retry <component>/<endpoint>`, with
`endpoint:retry`); a label edit changes nothing. `session.reprobe`
(at least `5m`, default never) lets a rejected session try once more on its own. A wrong password the
component answers with a declared refusal then costs one attempt rather than a retry every 30 seconds;
a component that hangs up on a wrong password without saying so costs three (the login budget), and
then reads `rejected` as `auth`. Anchor a prompt's regex (`^login: ?$`): a prompt is matched on a line
the component pauses on, and an unanchored one also matches a banner such as `Last login: `. A spec
whose session only commands would ride (no poll or listener to hold it) refuses at write. Over `ssh`
the session's auth step is the transport's own: `session.credential` names the secret input it
authenticates with (required: a `basic-auth` username and password, or an `ssh-key` username and
private key; a `session.login` beside it only answers keyboard-interactive questions, and a question
nothing declared answers, such as a one-time code, rejects the session), `session.pty` asks for a
terminal and `session.exec` runs a command as the channel instead of a shell. The host key is trusted
on first use and kept as the endpoint's `ssh-host-key`; when it changes the session reads `rejected`
naming both keys, which the gate carries as data (`endpoint-rejection` ends with the pinned and the
offered key). The console's **Trust this key** on the session's notice trusts the offered one after
a confirm ([held sessions](/guides/operator/collection/#a-changed-key)), and so does
`omniglass endpoint retry <component>/ssh --host-key <the offered key>`. While the refusal holds, the key must be
the offered one it names (any other is refused, 409, and changes nothing; retry without a key first
to have the node read a key the component presents since); with no refusal held, `--host-key`
declares a pin ahead of time, for a replacement whose key you already know.

A driver on `tls` speaks the line protocol over a TLS connection, its handshake the transport rung
([tls](/architecture/collection/#tls)). How the handshake judges the certificate is the driver's
`tls-mode` input, a string input the engine reads by name, like `host` and `port`: `verify` (the
certificate chains to one of the node's system roots and names the host), `pin` (the first public
key offered is trusted, any other refused) or `skip` (nothing is checked). The driver declares its
default, as the official `newtron-nvp-tls` declares `pin` for gear that signs its own certificate; a
driver that declares no `tls-mode` input verifies; and a spec that defaults it to `skip` refuses at
write, since skipping verification is only ever an operator's choice. An operator edits the mode in
place on the endpoint like any input, which moves its connection fingerprint, so a session a
certificate refused tries again under the new mode with no retry. A refused certificate reads
`rejected` with class `certificate` and its reason; for a pinned key that changed, the reason names
both keys and `omniglass endpoint retry <component>/tls --certificate <the offered key>` trusts the new one.

```yaml
transport: tls
inputs:
  - { name: host, kind: string, required: true }
  - { name: port, kind: number, default: "51326" }
  - { name: tls-mode, kind: string, default: pin }
```

A spec may also declare its **reachability ladder** (`ladder`), bottom up: `ladder.ping.enabled`
(whether to ping at all; a ping is informational only and never gates), `ladder.transport.rung` (the
gate: `connect` on the transport's port, the protocol's `handshake`, or a named `request` answered,
given as `ladder.transport.request`, where an open port cannot be detected; it runs as a line over
`tcp`, a datagram over `udp`, or a get over `snmp`) and
`ladder.session.enabled` (whether a login rung exists). Every field defaults from the transport
registry, so most drivers declare nothing, and a rung the transport cannot run refuses at write. A
cloud API is never pinged, but its 443 is checked open before its session runs:

```yaml
transport: http
ladder:
  ping: { enabled: false }
  transport: { rung: connect }
```

A refusal at a rung gates that rung and the ones above it for the endpoint; the rungs below keep
reporting ([the gate](/architecture/collection/#the-connection-fingerprint-and-the-layered-gate)),
except an snmp endpoint's get, which carries the community, so beneath its auth gate it is not sent
and reads down, rejected.

A udp driver speaks the line protocol as datagrams ([udp](/architecture/collection/#udp)). An open
udp port cannot be detected, so it must name its request rung, and it may declare a `listen-port`
input, the local port its endpoint binds, which its requests leave from and the component's pushes
arrive on (unset, the node picks one, which suits a component that answers to a request's source).
Every udp request declares its reply's shape (`reply`), since a datagram's answer is told from a
late reply to another ask only by it. A request may declare `retries` (udp only, 0 to 5), how many
times a lost datagram is re-sent within
the endpoint's timeout: a poll and the request rung default to 2, and a **command to none**, so
declare them only on a command that is safe to send twice (one that names its value, never a
relative step):

```yaml
transport: udp
inputs:
  - { name: host, kind: string, required: true }
  - { name: port, kind: number, default: "51327" }
  - { name: listen-port, kind: number }
ladder:
  transport:
    request: { line: "GET INPUT", reply: { prefix: "INPUT " } }
commands:
  - command_type: set-input
    request: { line: "SET INPUT ${value}", reply: { prefix: "INPUT " }, retries: 2 }
```

Over udp a `session` block may declare only `terminal` replies: nothing logs in or is held, and a
reply it names gates the endpoint at the transport rung.

An snmp driver's rung is a get of a standard OID, sysUpTime.0 unless it names another under
`ladder.transport.request.get` (numeric OIDs only; a line is refused), sent with the endpoint's
`community` secret. A v2c agent drops a wrong community silently, so it reads not answered and
retryable; an agent answering `authorizationError` reads rejected, class `auth`:

```yaml
transport: snmp
ladder:
  transport:
    request: { get: ["1.3.6.1.2.1.1.2.0"] }
```

An http or https driver sends real requests ([http and https](/architecture/collection/#http-and-https)).
A request declares its `method` (GET unless it says otherwise), `path`, `query`, `headers`, a `body`
(a JSON value, encoded by its `body_format`: `json`, `form`, `xml` or `text`) and the answer's
`format` (`json`, `xml`, `html` or `text`, declared rather than read off the `Content-Type`, which gear
mislabels). An emit locates its value with one selector the format admits: a `jsonpath` on a json
answer, an `xpath` on an xml answer (its prefixes declared on the request's `namespaces`) or on an html
page, a `regex` on any. A selector must find exactly one value: none, or more than one node, is a
fault naming the emit, and the rest still land. A command's request renders `${value}` and
`${arg.<key>}` where you place them, escaped for where they land (a path segment, a query value, a
JSON or xml body), and a JSON body leaf that is exactly `${value}` keeps the value's type, so an
intended volume is sent as a number. A request never names a secret and never sets `Authorization`
or `Cookie`: the credential is the session block's.

```yaml
transport: https
inputs:
  - { name: host, kind: string, required: true }
  - { name: port, kind: number, default: "443" }
  - { name: tls-mode, kind: string, default: pin }
  - { name: login, kind: secret, secret_type: basic-auth, required: true }
session:
  credential: login
  scheme: basic
polls:
  - name: audio
    schedule: { every: 30s }
    request: { path: /api/audio }
    emits:
      - { name: volume, extract: { jsonpath: volume } }
  - name: firmware
    schedule: { every: 1h }
    request: { path: /api/status.xml, format: xml, namespaces: { d: "urn:polaris:dsp:1", sys: "urn:polaris:system:1" } }
    emits:
      - { name: firmware-version, extract: { xpath: "/d:status/sys:device/sys:firmware" } }
commands:
  - command_type: set-volume
    request: { method: POST, path: /api/audio/volume, body: { volume: "${value}" } }
    settle: { from: response, success: { regex: '"ok":\s*true' } }
```

`session.scheme` says how the credential `session.credential` names rides every request: `basic` (a
`basic-auth` secret), `bearer` (a `bearer-token`), `api-key` (an `api-key`, placed by
`session.api_key: { in: header, name: X-Census-Key }` or `{ in: query, name: apikey }`), or `digest`
(a `basic-auth` secret answered as HTTP digest, MD5 or SHA-256: the node holds the component's
challenge, answers a stale nonce afresh, and never sends the password). A 401 to a
credentialed request reads `rejected`, class `auth`, and nothing credentialed is sent again until the
endpoint's connection fingerprint changes; a `session.terminal` rule may name a `status` (a gear's
403 for a wrong credential) or a body match. Any other 4xx faults that request alone and waits its
interval; a 408, 429 or 5xx retries on the backoff, no sooner than a `Retry-After`; a redirect is
not followed. The reachability probe never sends the credential, so an http endpoint's
`endpoint-authenticated` is proven by its polls; a driver whose commands carry a credential needs a
poll to carry it to the node. Over `https` the `tls-mode` input and the pinned key work as over `tls`
(above), and `omniglass endpoint retry <component>/https --certificate <the offered key>` trusts a changed key.

Gear whose web API signs in once declares `session.login` instead of a scheme
([logging in over http](/architecture/collection/#logging-in-over-http)): the login dialog a telnet
driver declares, its steps requests. A step names only the secret fields it sends
(`${secret.login.password}`); the node keeps the session it opens (the component's cookies, the
values `capture` locates in the last answer, the `headers` that send them) in its memory alone, per
endpoint, and logs in again only when a request draws a 401, a redirect to the declared login
`path`, or the declared `expired` text, then replays that request once. A form login:

```yaml
session:
  login:
    path: /login
    steps:
      - request:
          method: POST
          path: /login
          body_format: form
          body: { username: "${secret.login.username}", password: "${secret.login.password}" }
```

A token login captures the token and sends it as a bearer; `expiry` renews it before it lapses:

```yaml
session:
  login:
    steps:
      - request: { method: POST, path: /api/login, body: { username: "${secret.login.username}", password: "${secret.login.password}" } }
    capture: [{ name: token, extract: { jsonpath: token } }]
    expiry: { jsonpath: expires_in }
    headers: { Authorization: "Bearer ${session.token}" }
    expired: { regex: "token expired" }
```

A login the component refuses (a 401, a redirect back to the login page, another 4xx, or a last
answer without its `ready` text or a value it captures) is tried once: the endpoint reads
`rejected`, class `auth`, with the component's reason, and logs in again only when the fingerprint
moves (fix the secret in place with `omniglass secret update`, or edit the endpoint's inputs). A
login answered with a 5xx or a dropped connection is retried, but the third in a row reads
`rejected` too. A session refused right after a fresh login reads `rejected`, never a second login.
`expiry` is seconds, held between 10 seconds and 30 days; zero or a missing value means none. A
login declares no scheme beside it, its step paths open with a literal `/`, and a header it sends
names only a capture, never a secret, `Cookie` or a header a poll or command sets.

A component whose web page updates live from a server-sent events stream declares the stream in its
session block, and listeners claim its events ([a server-sent events
stream](/architecture/collection/#a-server-sent-events-stream)). The stream is a `GET` (a `path`, a
`query`, `headers` other than `Accept` and `Last-Event-ID`, which the node sends), read as `json`
(the default) or `text`, and it rides the session's scheme or login like every request. A listener
over http declares no `arm` (a stream has no upstream); its `match` may name the event (`event:
change`), and carry one data rule: a `prefix`, a `regex`, or a JSON `field` (`{ path: path, equals:
status.audio.volume }`, or a `regex` in place of `equals`):

```yaml
session:
  credential: login
  scheme: basic
  stream: { path: /api/events }
listeners:
  - name: volume-change
    match: { event: change, field: { path: path, equals: status.audio.volume } }
    emits:
      - { name: volume, extract: { jsonpath: value } }
```

The node holds the stream as the endpoint's session: it reads `up`, `recovering` after a drop (it
resumes from the last event id after the server's `retry`, landing nothing twice), and `rejected`
when the component refuses its credential, which also gates the endpoint's polls and commands. A
stream needs a listener (with none, nothing holds it), and a listener over http needs the stream.

A component whose control API is a websocket declares `transport: websocket` (or `wss`, over the
TLS handshake, with a `tls-mode` input as `tls` and `https` take), and one held socket carries its
polls, its commands and its listeners ([websocket and wss](/architecture/collection/#websocket-and-wss)).
`session.upgrade` is the `GET` that opens it (a `path`, the root when absent, a `query`, `headers`,
never one the handshake owns), riding the session's scheme (`basic`, `bearer` or `api-key`; not
`digest`) or an http login. A request is a frame: a `line` sent as is, or a JSON `body` (a
whole-leaf `${value}` keeps the intended value's type). Its `reply` may `correlate` by a field: the
node writes a fresh id into that field of the body each time it sends it, and the answer is the
frame carrying the id back, so pushes and other answers between never confuse it. A listener's
`arm` is the subscribe frame, sent on every connection (one line, compact JSON), and its `match`
claims a push as any held session's does, a JSON `field` match the usual choice:

```yaml
transport: wss
session:
  credential: login
  scheme: basic
  upgrade: { path: /api/ws }
listeners:
  - name: volume-change
    arm: ['{"type":"subscribe","paths":["status.audio.volume"]}']
    match: { field: { path: path, equals: status.audio.volume } }
    emits:
      - { name: volume, extract: { jsonpath: value } }
commands:
  - command_type: set-volume
    request: { body: { type: set, path: status.audio.volume, value: "${value}" }, reply: { correlate: id, error: { regex: '"type":"error"' } } }
    settle: { from: response, extract: { jsonpath: value } }
```

A component that answers a request without echoing an id declares a `field` reply instead (`reply:
{ field: { path: type, equals: result } }`), or a `prefix` or `regex` for a text frame. The socket
reads `up`, `recovering` when the component drops it or stops answering the node's pings, and
`rejected` when the upgrade is refused with a `401` or a `403`, after one attempt.

A telnet driver:

```yaml
session:
  login:
    steps:
      - { expect: { regex: "^login: ?$" }, send: "${secret.login.username}" }
      - { expect: { regex: "^Password: ?$" }, send: "${secret.login.password}" }
    ready: { prefix: "Newtron Panel 7 NVP ready" }
  terminal:
    - { class: auth, match: { prefix: "auth rejected" } }
  reprobe: 1h
```

A poll's `schedule.every` is how often the node asks the device, at the node's heartbeat
granularity (a `60s` poll on a 30s heartbeat runs every other tick). A poll whose fetch fails
(the device rebooting, say) retries sooner, on a backoff that starts at the heartbeat and doubles
up to the interval, so a daily poll is not lost for a day. A listener's pushes land as they arrive
whatever the schedule. Changing a spec (or its version) reaches the endpoints already attached through it: their tasks
re-derive in the update's own transaction, under its one audit row, and the next poll runs the
new spec; an official spec a new release ships reaches them at boot the same way.
A row without a spec is a **stub** and cannot be attached; specs are
authored over the API or CLI (`omniglass driver create --spec ...`) and **validate at write**,
so a spec that references an unregistered sample name, an unknown secret shape, or a broken
template refuses with the fault named (422). Attaching a driver to a component happens on the
component's own detail ([nodes and reachability](/guides/operator/collection/)): the spec
derives the endpoint and its tasks. Its inputs (the host, the port, an attached secret's name) then
edit in place on the endpoint's blade or with `omniglass endpoint update <component>/<endpoint> --inputs ...`, checked
against the spec's declared inputs; no re-attach. Clearing an optional input restores its default;
a required one cannot be cleared.

A driver is consumed by the [product](/guides/admin/products/) catalog: a `product` references
its driver through an optional `driver_id` to say which driver reads it, chosen from a driver
picker on the product's create and edit forms, and three of the shipped official products bind a
driver this way. It is a leaf catalog beside the vendor registry. See
[core entities](/architecture/core-entities/) for where it sits in the fleet model.

- **New driver** (with `driver:create`, an admin permission) opens a create drawer: give it a
  **name** (unique tenant-wide, e.g. `snmp-generic`), a **label**, and,
  optionally, a **version**.
- Pick a row to open its **detail blade**. The footer **Edit** pencil (with `driver:update`) edits
  the label and version; the **name** is fixed, since a catalog row carries no rename
  (`:rename` is a component, system, location, and principal group affordance). **Delete**
  (with `driver:delete`) removes the row, behind a confirm. A verb you lack greys just that
  button, its hover reason naming the permission (`Requires driver:update`, `Requires driver:delete`);
  the pair never disappears.
- An **official** row is always read-only: the blade keeps the Edit and Delete pair in place,
  greyed, with the reason on hover: "Official: ships with Omniglass and updates with it."
  Omniglass ships a starter set of official drivers (Generic SNMP, Newtron NVP, and Newtron NVP
  over Telnet, its login a session block, over SSH, and over TLS, pinning the panel's key, the Kestrel
  Device API over https with basic auth, HTTP JSON over http with a header key, Newtron Panel Web
  over https behind its form login, and Boreal Edge Web Admin over http with digest, all with real
  specs), upserted idempotently at boot so the
  shared set cannot drift install to install; add a custom driver for anything else.
- **Delete** refuses while an **endpoint is attached** through the driver (422): an attach
  derives real collection work from the spec, so the driver cannot vanish out from under it.
  The [product](/guides/admin/products/) link stays soft: `product.driver_id` is
  `on delete set null`, so deleting an unattached driver detaches it from those products
  (their driver clears) rather than blocking. Official rows stay refused outright (422).

Minting a driver is admin-gated; the picker that consumes it lives on the
[product](/guides/admin/products/) create and edit forms. The same operations are `omniglass driver
list/get/create/update/delete` from the CLI (see the [CLI reference](/reference/cli/)).

## Check, preview, and run a poll now

Three commands take the guesswork out of writing a spec, each a route an agent can call as an
[MCP tool](/reference/mcp/) too:

```sh
# Check a spec as a save would, writing nothing: every fault, each on its function
omniglass driver validate --spec "$(cat display-web.json)" | jq '.findings[] | {function, message}'

# Run its polls once through a node against the component, storing nothing
omniglass driver preview --node edge-hq --spec "$(cat display-web.json)" \
  --inputs '{"host":"10.20.4.31","login":"display-login"}' | jq '.results[] | {poll, samples, faults}'

# Once attached, run an endpoint's poll now; its samples land
omniglass endpoint poll run 'hq.west.level-2.huddle.$comp.display-1/http' identity
```

Validate needs only `driver:read`. A preview (`driver:preview`) answers with the samples each poll
extracted, each emit that did not locate, and an excerpt of the raw answer to write the next extractor
against, masked of the secret it used; a secret input must be one you may reveal, since the preview
hands it to the target you name. A preview runs a poll that is its own request (http, https, snmp, a
tcp line); a poll riding a held session (telnet, ssh, a websocket, a tls line, a login session) is
attached first and run now (`endpoint:run`), which lands its samples and is refused, with the reason,
while the endpoint is gated. Both are operator grants, audited
([collection](/architecture/collection/#on-demand-validate-preview-and-run-a-poll-now)).
