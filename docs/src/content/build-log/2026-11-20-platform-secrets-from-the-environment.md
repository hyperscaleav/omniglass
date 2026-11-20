---
title: "platform secrets from the environment"
date: 2026-11-20
refs: [2223]
---

A platform secret can now be declared by the server's environment. `OMNIGLASS_SECRET__<NAME>` holds
one JSON object, `{"type": "api-key", "fields": {"key": "..."}}`, checked against its type at boot
and held in memory only: nothing is sealed or written. omnibot's key, the mail relay's login and an
endpoint's secret input take it as they take a stored platform secret, behind the same
`secret:bind`; the directory lists it with `source: env` and no fields, the console reads it as
**From environment**, and an update, delete or reveal is a 409 naming the variable. A stored secret of
the same name fails the boot.

With the env settings layer, pointing omnibot at Bedrock's OpenAI-compatible endpoint is five
variables. `omniglass lab run` forwards the secret variables beside the settings ones, the binary
loads `.env` and then `.env.local` under the shell, so those five in `.env.local` are the whole setup,
and `--omnibot-key-env` is now an alias for an env secret rather than a key the seed seals.
