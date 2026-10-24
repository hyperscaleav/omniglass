---
title: "Slice 3a: the generated CLI"
date: 2026-06-28
refs: []
form: bullet
---

- **Slice 3a: the generated CLI.** The second stage of the [generation pipeline](/contributing/api-first/)
  lands: `make gen` now runs `cmd/cligen`, which reads the committed `api/openapi.json` and emits the cobra
  command tree (`internal/cli/api_gen.go`, committed and reviewed like the spec). One command per operation,
  derived from the AIP path and method: `omniglass location list/get/create/update/delete`, `role list`,
  `auth me`, `healthz`; path parameters are positional args, the request body becomes `--flags`, and `--help`
  plus the example come from the operation's summary and description. A `:verb` custom method maps to
  `<resource> <verb> <id>` generically, so future custom methods surface with no generator change. The
  generated tree composes with the hand-written commands (the `server`/`migrate` run modes and the trusted
  `bootstrap` lane) on one root through a stable seam (`internal/cli/api_hooks.go`: the JSON-over-HTTP client
  and the shared `--server`/`--token` flags), so regeneration never touches a hand-written command. The CLI is
  a client of the API like any other: it carries a bearer token and the server enforces the same capability
  and scope. Proven by an end-to-end test that builds the real binary, bootstraps an owner with the
  hand-written command, and drives the generated location commands against a live server (create, scoped list,
  get, a non-disclosing 404 as a non-zero exit, delete, and `--help`). Deliberate thin cuts: JSON output only
  (no table rendering), and a generic client rather than a per-type typed one. Deferred: the typed SPA client
  and UI (Slice 3b), shell completion, multi-server contexts, and the YAML JSONSchema target.
