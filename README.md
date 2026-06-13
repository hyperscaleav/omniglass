# Omniglass

**Omniglass is an open observability and control plane for AV and IT estates, and a
place to learn how one is built.** It is a single Go binary over a BYO PostgreSQL
database: collect telemetry from devices, type it into owned datapoints, model health
across systems and locations, alarm on it, and act.

It is two things at once, on purpose:

- **A working tool.** Run it, point it at your fleet, and operate.
- **A learning tool.** The same binary serves an interactive docs and concept site at
  `/docs`. Concepts and data flows are taught *in the product*, against real or
  simulated data, not just described in a wiki.

> **Status: early, and public from the first commit.** Omniglass is being rebuilt in
> the open, one vertical slice at a time. Expect the surface to move. The architecture
> is published ahead of the code at
> [omniglass.hyperscaleav.com/docs](https://omniglass.hyperscaleav.com/docs).

## Design principles

- **Single binary, run modes.** One artifact runs as `server`, `node`, or `migrate`.
- **BYO Postgres.** Standard managed PostgreSQL. No bespoke datastore.
- **API first.** The Go API is the contract; the web app and CLI are generated clients.
- **Test first.** A behavior change ships with the test that failed before it.
- **Docs with everything.** A feature is not done until the docs that teach it ship
  with it.
- **Functional and pedagogical.** Every operator surface should also teach the concept
  it operates on.
- **Primitive first.** Build the reusable primitive, then consume it; do not inline a
  one-off where a primitive belongs.

## Quick start

> Filled in as the first slices land. The shape:

```bash
make build        # build the omniglass binary (web + docs embedded)
make pg-up        # ephemeral dev Postgres
make migrate      # apply schema
make run          # start the server; docs served at http://localhost:PORT/docs
```

## Repository layout

```
cmd/omniglass     entrypoint (server | node | migrate)
internal/         the application (storage gateway, collection, rules, api, ...)
db/migrations/    dbmate schema migrations (pure DDL)
docs/             the Hugo (Hextra) docs + learning site, embedded into the binary
web/              the operator SPA
```

## Documentation

The architecture, concepts, and contributor guide live in [`docs/`](docs/) and are
published at [omniglass.hyperscaleav.com/docs](https://omniglass.hyperscaleav.com/docs).
Start with the architecture spine.

## Contributing

All work happens through pull requests against `main`, tracked as GitHub issues under
epics. Read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a PR. The short version:
test-first, ship the docs, conventional-commit PR title, green CI, one reviewer.

## License

Omniglass is licensed under the **GNU Affero General Public License v3.0**. See
[LICENSE](LICENSE). If you run a modified Omniglass as a network service, the AGPL
requires you to offer your users the corresponding source.
