---
title: The API
description: "Building against the Omniglass HTTP API: where its contract lives, how to call it, and the clients generated from it."
sidebar:
  order: 1
  badge:
    text: Built
    variant: success
---

The API is the only way in. The console, the [CLI](/developer/cli/), the [MCP server](/developer/agents/)
and your own scripts all call the same HTTP routes, so each passes the same permission and scope
checks, and anything one can do another can.

## One source of truth

The routes are Go structs (Huma). From them `make gen` writes an OpenAPI 3.1 document, and from that
document the typed console client, the CLI commands, the MCP tools and the YAML JSONSchema. So there
is no hand-kept list of routes to drift from the code: the [API reference](/reference/api/) is the
rendered document, and a new route shows up in every client on the next generation.

## Calling it

Send a bearer token in the `Authorization` header. Mint your own with the console's Create token
action or `omniglass auth create-token`; the [CLI page](/developer/cli/#authentication) covers the first
owner, and [agent tokens](/developer/agents/#give-the-agent-a-token-of-its-own) are a narrower kind for
software acting on your behalf.

```sh
curl -H "Authorization: Bearer $OMNIGLASS_TOKEN" https://omniglass.example.com/api/v1/locations
```

Every route is gated by a `<resource>:<action>` permission, and every query a route runs is narrowed to
the scope your grants reach, so a token sees exactly what its user does.

## The shape of the contract

- **Resources and `:verb` methods.** Routes are AIP-style: a collection (`/locations`), an item by
  name or id, and a custom method after a colon for an action that is not a plain update.
- **Names address.** A resource you type the name of answers to it; the id it returns survives a rename.
- **Lists** filter, order and page by cursor.
- **Errors** are one `problem+json` envelope, with the field a 422 refused named.
- **Writes** are audited, and long-running work returns an action you read back rather than holding the
  request open.

The full contract, with the reasoning behind each choice, is on the
[API architecture page](/architecture/api/).

## Where to go next

- [The CLI](/developer/cli/) for scripting without writing HTTP.
- [Agents and MCP](/developer/agents/) to put an AI agent on the same API.
- [Contributing](/developer/contributing/) to change the API itself: a route starts as a test and a Huma
  struct, and `make gen` does the rest.
