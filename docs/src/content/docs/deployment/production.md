---
title: Running in production
description: "What a production Omniglass needs: BYO PostgreSQL sized for telemetry, the three chart values, upgrades and rollback, the encryption key, single sign-on through your identity provider, and what is still to come."
sidebar:
  order: 4
  badge:
    text: Partial
    variant: note
---

A production deployment is the same binary and [chart](/deployment/helm/) as a preview, with three
values changed: a database you own, no auto-created owner, and a key you keep. The server runs as one
replica, the supported shape; [scaling and deployment](/architecture/scaling/) holds the deferred
fork for more. Once it is up, mint the first owner with `omniglass bootstrap`
([the CLI guide](/developer/cli/#authentication)), then move to the [Admin](/admin/) section.

## The three values

```yaml
postgres:
  enabled: false
externalDsn: "postgres://user:pass@managed-db.example.com:5432/omniglass?sslmode=require"
bootstrap:
  enabled: false
```

The bundled Postgres is an `emptyDir` and does not survive a pod restart, so it is for previews only.
`externalDsn` carries credentials: the chart makes a Secret from it and injects `OMNIGLASS_DSN` into the
server and the `migrate` init container. Supply it with `--set-file externalDsn=<file>` or fill it from
your secret tooling before install.

:::design[Target design, tracked in #1291]
The bundled Postgres keeps its data and says it is for demos only.
:::

## What to back up

Two things hold the state, and a restore needs both:

- **The PostgreSQL database.** Take your database's own backups; the chart never touches an external one.
- **The encryption key.** Every stored secret is sealed under it, and losing it makes them unreadable
  with no recovery. Read it out after the first install and keep it outside the cluster
  ([the encryption key](/deployment/helm/#the-encryption-key)).

The server's data volume holds the embedded bus's store, so a batch acknowledged before a restart lands
after it ([durable state](/deployment/helm/#the-data-volume)). It is not the system of record.

## Upgrades and rollback

Every rollout is gated on the schema. The `migrate` init container applies pending migrations under a
Postgres advisory lock, so replicas take turns, and it refuses a database a newer release migrated, so a
pod fails there rather than serve on a schema it does not know ([how it boots](/deployment/helm/#how-it-boots)).

Migrations only move forward. Back up the database before an upgrade; a `helm rollback` to an image older
than the schema stops at `migrate`, so roll back the image only together with that backup, or roll forward.

:::design[Target design, tracked in #1290]
A readiness probe that reflects the bus and its consumer, so a rollout waits on more than the process being up.
:::

## Sizing PostgreSQL

Postgres ships with settings sized for a small machine it shares with other work, and a fleet's
telemetry is a steady stream of writes. Set these in `postgresql.conf` (or as `-c` flags on the
`postgres` command). The scale lab runs the same settings, its `shared_buffers` also held to what
its simulated fleet leaves of the host, so a figure it measures is one a sized production database
would see:

| setting | value | why |
|---|---|---|
| `shared_buffers` | a quarter of the host's memory, up to 8 GB | the stock 128 MB holds a fraction of the recent telemetry, so reads that should hit memory go to disk |
| `max_wal_size` | `8GB` | the stock 1 GB had a 3,000-system soak checkpointing on WAL volume four times as often as on its timer, with commits stalling behind each checkpoint; leave 8 GB of disk free for it beside the data |
| `checkpoint_timeout` | `15min` | with the larger WAL, spaces checkpoints out to one each 15 minutes |
| `wal_compression` | `lz4` | compresses the full-page images each checkpoint forces into the WAL, for a little CPU |
| `track_io_timing`, `track_wal_io_timing` | `on` | records time spent in reads and WAL writes, so `pg_stat_io` says when the disk is the bottleneck |

Leave `synchronous_commit` on. Turning it off trades durability for throughput: a crash loses the
last transactions the server already acknowledged, and an acknowledged sample that disappears is the
loss the platform exists to measure. `track_commit_timestamp=on` is needed only by
[`omniglass lab measure`](/developer/lab/#measuring-a-scale-lab), which reads ingest lag from commit
times.

In a container, `shared_buffers` is not held in `/dev/shm`: Postgres maps it as anonymous memory, so
count the buffers against the container's memory limit. `/dev/shm` holds the segments parallel
queries share, and Docker's default 64 MB can be too small for a large parallel hash join, which then
fails with `could not resize shared memory segment`; raise it with `docker run --shm-size` or
Compose's `shm_size`.

:::design[Target design, tracked in #1293]
Resource profiles and a connection pool cap, from measurement. Until then, size the database as above
and watch the server's own memory.
:::

## Hardening and TLS

:::design[Target design, tracked in #1294 and #1295]
An optional network policy and restricted pod security (#1294), and the server terminating TLS itself
and verifying a client certificate (#1295). Until then terminate TLS at your ingress or proxy.
:::

When the server runs behind a reverse proxy, name the proxy ranges in `OMNIGLASS_TRUSTED_PROXIES` (for
example through `server.extraEnv`) so forwarded client addresses are honored.

:::design[Target design, tracked in #1231]
The trusted-proxy default is loopback only, and the chart declares its ingress range.
:::

## Single sign-on

People can sign in through your organization's identity provider over OpenID Connect (the
authorization code flow with PKCE). One provider is connected per install. Sign-in starts at
Omniglass, never at the provider, and an identity signs in only once it is linked to a person
([linking](/admin/users/)); nothing links by email. Owners keep password sign-in as the way in when
the provider is down.

**1. Register Omniglass at the provider** as a confidential web application:

- Redirect (callback) URI: the console's address plus `/auth/oidc/callback`, for example
  `https://omniglass.example.com/auth/oidc/callback`. Omniglass builds it from the address the request
  reached it at, believing a forwarded `https` only from a peer in `OMNIGLASS_TRUSTED_PROXIES`, so set
  that behind a TLS-terminating proxy or the callback reads `http://` and the provider refuses it.
- Scopes: `openid email profile offline_access`. `offline_access` is what makes the provider issue a
  refresh token, which the re-check below needs; some providers grant it from a setting rather than a
  scope (Okta: allow the refresh token grant; Google: it is `access_type=offline`, which Google gives a
  confidential client on first consent).
- Note the client id and client secret it gives you.

**2. Store the client** as a platform secret of type `oauth2-client`, its `client_id` field the
client id and its `client_secret` field the secret, from **Admin > Secrets** or the CLI, or declare it
from the server's environment ([environment secrets](/deployment/container-image/#configuration)).
The two rotate together because they are one secret.

**3. Point the `sso` settings at it.** From **Admin > Settings**, the CLI, the settings file or the
environment (`OMNIGLASS_SETTING__SSO__ISSUER`, `OMNIGLASS_SETTING__SSO__CLIENT_SECRET`):

```sh
omniglass setting update sso --body '{"issuer": "https://login.example.com/realms/corp", "client_secret": "corp-sso"}'
```

or, with nothing to create in the console, entirely from the server's environment: the two settings
below, and the client as an env-backed platform secret named `corp-sso` (an `OMNIGLASS_SECRET__` variable
holding `{"type":"oauth2-client","fields":{"client_id":"...","client_secret":"..."}}`).

```bash
OMNIGLASS_SETTING__SSO__ISSUER=https://login.example.com/realms/corp
OMNIGLASS_SETTING__SSO__CLIENT_SECRET=corp-sso
```

- `issuer` is the provider's issuer exactly as its tokens carry it (its discovery document is
  `<issuer>/.well-known/openid-configuration`). Empty turns single sign-on off.
- `client_secret` names the `oauth2-client` secret. Naming it, or moving `issuer` while one is bound,
  from the console or API takes `secret:bind` on that secret, since every sign-in sends it to the
  issuer's token endpoint; the settings file and environment are trusted as deployment config.
- `scopes` (default `openid email profile offline_access`), `groups_claim` (default `groups`, the claim
  [identity provider groups](/admin/access/#identity-provider-groups) are read from), `recheck_minutes` (default 15) and `recheck_grace_minutes` (default
  60) are explained below.

**4. Let the server reach the provider.** Discovery, the key set and the token exchange go through
the server's outbound client: TLS verified, no redirect followed, no proxy from the environment, every
call bounded. A cloud provider on the public internet needs nothing more. A provider on your own
network (an on-premises Keycloak or ADFS) is reached only when the notification settings' `egress_allow` names its
host or range, exactly as an internal webhook is, and plain `http` is admitted only to a host named
there.

**5. Link people.** An administrator links each person to their identity from the person page, or the
person links their own from their profile with their password (the only path for an owner). The
sign-in page then shows **Sign in with SSO**.

### When someone leaves

Omniglass learns of a removal only by asking, and there is no SCIM push yet. Every `recheck_minutes`
(15) the server redeems each link's refresh token at the provider. When the provider refuses it
(`invalid_grant`), every session and API token the person made through single sign-on is revoked in
one audited step, and the person can sign in again only if the provider lets them. So a removed person
keeps access for **up to 15 minutes after the provider starts refusing**, and longer if the provider
is slow to. **Disabling the person in Omniglass is the instant cut-off.**

If the provider cannot be reached or answers with an error, nothing is revoked: the server retries
at the next pass, and once a link has gone `recheck_grace_minutes` (60) without an accepted refresh its
sessions and tokens are refused until one is, so an outage suspends SSO access for a while and
destroys nothing (owners still have their passwords). A provider that issues no refresh token gets no
long-lived access: a session through it lasts `recheck_grace_minutes`, and minting an API token from
it is refused. A provider that issues the refresh token only on first consent (Google does) keeps
working: a later sign-in that brings none keeps the one the link already holds. An API token
an owner minted from a password session is not an SSO credential and is not re-checked.

How soon a refusal comes depends on the provider (as each one's documentation stood in November
2026):

| Provider | When it refuses the refresh | Runbook when someone leaves |
| --- | --- | --- |
| Entra ID | Once an admin revokes the person's sessions; disabling the account alone is not documented to revoke refresh tokens. A group change can take up to a day to reach issued tokens, and a B2B guest's tokens are not revoked in the resource tenant. | Disable the account **and** revoke its sessions. |
| Okta | Once the person is deactivated or suspended (reported, not in Okta's reference). The default refresh token lives without limit with a seven-day idle expiry, which the re-check keeps alive. | Deactivate or suspend the person. |
| Google | Suspending an account is not documented to revoke a third party's refresh token, and Google sends no groups claim. | Suspend the person at Google **and** disable them in Omniglass. |

The model, and what is deferred (SCIM deprovisioning, several providers at once, provisioning
and an enforce mode for everyone but owners), is
[ADR-2361](/architecture/decisions/#adr-2361-single-sign-on-links-a-person-to-an-identity-provider-subject-never-by-email).
Mapping a provider's groups to Omniglass groups is built: see
[identity provider groups](/admin/access/#identity-provider-groups).
