---
title: "http polls and commands send real requests, read through one extraction, authenticated by a declared scheme"
date: 2026-10-24
status: "Accepted"
summary: "http becomes a full collection transport (#902, slice 1 of epic #1113, its decisions 1 and 4 to 7): a driver's http polls and commands declare method, path, query, headers, a body and the response format, rendered injection-safe; the one Extract primitive gains a namespace-aware `xpath` over xml and parsed html beside `jsonpath`, `regex`, `oid` and `key`, one value per selector; auth is the session block's `scheme` (basic, bearer, api-key, placed by the driver) over the new `bearer-token` and `api-key` secret types; a credentialed 401 is a terminal `auth` at the session rung, proven by the credentialed exchanges while the probe stays credential-free; the status table splits gating, waiting and backing off; `https` is its own registry entry over the shared TLS primitive; a listener on a transport with no listen path refuses at write (#1115)"
refs: [902, 1113, 1115]
---

- **Date:** 2026-10-24 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [nodes](/architecture/nodes/), [glossary](/architecture/glossary/), [drivers](/guides/admin/drivers/), [simulation](/architecture/simulation/)
- **Decision:** (#902, under the architect's approval of epic #1113, decisions 1 and 4 to 7.)
  1. **The request.** An http or https `request` declares `method` (GET by default), `path`,
     `query`, `headers`, a `body` (a JSON value rendered in its `body_format`: json, form, xml or
     text) and the response's `format` (json, xml, html, text; declared, never sniffed, since gear
     mislabels its content type). Polls and command bindings share it. Rendering is
     injection-safe: a path substitution is escaped as one segment, a query value is encoded by the
     client, a header value with a control character refuses, a JSON body is rendered leaf by leaf
     and then encoded (a leaf that is exactly one reference keeps the value's type, so an intended
     metric rides as a number), and an xml body's substitutions are escaped as text. A request may
     not set the credential headers or the ones the client owns, and never names a secret: a
     credential enters only through the session's scheme, one fingerprint input and one scrub.
  2. **One extraction primitive** (decision 1). `Extract` gains `xpath`: XPath 1.0 over an xml
     answer, its prefixes declared on the request (`namespaces`), or over an html answer parsed as a
     browser would, beside `jsonpath` (reused, the dotted path), `regex` (any body as text), `oid`
     and `key`, exactly one per emit, each bound to the format at write. A selector yields one
     value: none or many is a fault naming the count, never a silent first. A response is parsed
     once however many emits read it, and a response-settled http command reads its reply through
     the same primitive. CSS selectors are #1118.
  3. **Auth is declared in the session block** (decision 6). `session.credential` names the secret
     input and `session.scheme` how it rides: `basic` (`basic-auth`), `bearer` (the new
     `bearer-token`) or `api-key` (the new `api-key`), whose placement (`in: header` or `query`)
     and name the driver declares (`session.api_key`), so a secret type stays a pure shape. The
     session block is already a fingerprint input, so every credential path is. A generic driver's
     placement as inputs is #1124.
  4. **The status table** (decision 5). A rule the driver declares terminal decides first
     (`session.terminal` may match a `status`, a body, or both); then 2xx is success; a 401 to a
     credentialed request is a terminal `auth`; a 408, 429 or 5xx backs off, never sooner than its
     `Retry-After` and never later than the poll's interval; a redirect is not followed; and every
     other 4xx (a 403 unless declared) faults the request alone, waiting its interval and gating
     nothing. A body past 1 MiB (64 KiB for a command's reply) faults naming the cap.
  5. **The session rung is proven by the credentialed exchanges** (decision 7). A credentialed
     answer lands `endpoint-authenticated` (yes, or no on the refusal; a 403 or a 5xx proves
     nothing), and a refusal gates the session rung for the fingerprint, beneath which no
     credentialed poll or command is sent, across a restart. The probe sends no credential, so
     reachability stays observed beneath the gate. A credentialed probe request is #1120.
  6. **https is its own registry entry** (decision 4). It needs both things ADR-0160's placement
     rule names (its rung is the handshake, and a component may serve a plain web port beside a TLS
     one), so it joins the registry over the shared TLS primitive: the `tls-mode` input, the public
     key pinned on first use as `tls-certificate` (one pin per endpoint, shared by its probe, its
     polls and its commands), `:retry` with `certificate`, and the handshake time and certificate
     facts from the probe. A transport over the handshake is the registry's `tls` flag, never a
     second list. One transport per driver stays.
  7. **Commands over http.** The command delivery carries the rendered request and never a
     credential; the node applies the endpoint's scheme from what its worklist delivered, so a
     credentialed http driver that declares commands and no poll refuses at write.
  8. **The registry tells the truth.** http no longer claims `held` (no code path held an http
     session); SSE (#1038) makes it so. A listener on a transport with no listen path (anything but
     a held line transport or udp) refuses at write, where it derived a task no node ran (#1115).
  9. **The simulated faces.** An http face can demand a bearer token or a query-parameter key; an
     https face serves an ECDSA key derived from the simulated component's id (a browser opens the
     page; an Ed25519 certificate it would refuse), the same on every run, so a pinned endpoint
     survives a simulator restart; the DSP serves its status as namespaced XML.
- **Context:** http was a reachability probe only: `checkPollShape` refused every http poll and a
  command binding on http had no actuation wire, though most AV gear's richest surface is its web
  API and its web page. The architect's direction (2026-10-24) made it a full transport, one
  extraction primitive with format-specific selectors rather than an extractor per format, and the
  epic's decisions settled the forks; ADR-0160 had foreseen https as a capability on http, which
  its own placement rule did not support once the rung and a second web port were weighed.
