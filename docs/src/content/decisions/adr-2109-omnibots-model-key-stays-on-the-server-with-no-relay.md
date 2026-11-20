---
title: "omnibot's model key stays on the server, with no relay"
date: 2026-11-18
status: "Accepted"
summary: "omnibot's model is an OpenAI-compatible provider the assistant settings name, its key a $platform secret the server unseals in process for each run and sends only to the model. The relay route first specified is dropped: the server already runs the loop, and the CLI moves onto that run, so no client ever needs the key"
refs: [2109, 2121, 1166]
---

- **Date:** 2026-11-18 | **Status:** Accepted | **Pages:** [AI](/architecture/ai/), [settings](/architecture/settings/), [connect an agent](/developer/agents/#pointing-omnibot-at-a-model)
- **Decision:** (#2109, on the server-run loop of [ADR-2121](/architecture/decisions/#adr-2121-the-server-runs-omnibot-as-the-person-streaming-ag-ui).)
  1. **One adapter, chosen by URL.** OpenAI-compatible chat completions with tool calls, OpenRouter
     by default; OpenAI, Ollama, vLLM and LM Studio are the same adapter at another `base_url`. The
     call leaves through the egress guard like any outbound call.
  2. **The key is a secret by name.** `assistant.key_secret` names a `$platform` api-key secret, as
     `mail_secret` names the mail relay's. The server unseals it at the start of each run and sends it
     only as the model call's bearer; a provider error that echoes it is redacted before it reaches
     an event or a log line. **Amended (#2223):** the secret may also be one the server's
     environment declares (`OMNIGLASS_SECRET__<NAME>`, [ADR-2223.2](/architecture/decisions/#adr-22232-a-platform-secret-may-be-declared-by-the-servers-environment));
     it is still a `$platform` secret by name, behind the same `secret:bind`.
  3. **Chosen per run.** A configured provider whose key resolves; else `OMNIGLASS_OMNIBOT_REPLAY`;
     else 503. A provider configured with a key that no longer resolves is a 503 naming the secret,
     not a silent fall back to a recording.
  4. **The CLI runs on the server.** `omniglass omnibot` drives `{name}:run` and renders its AG-UI
     stream, confirming an ask on the terminal and answering it with a resume run. `--replay` keeps
     the local loop of [ADR-1166](/architecture/decisions/#adr-1166-omnibot-is-an-mcp-client-on-its-own-agent-token-confirming-in-its-client)
     for tests and tapes.
- **Context:** #2109 first specified a relay route that would forward a client's model calls with
  the install's key added. ADR-2121 then put the loop on the server, where the key is already in
  reach, which leaves a relay with nothing to do but widen the surface the key crosses.
- **Not chosen:**
  - **The relay.** A route that forwards arbitrary prompts on the install's key is a second way
    to spend it, outside the run's permission, policy and audit.
  - **A key per person.** Each person bringing a key spreads a credential the install could hold
    once; a person-scoped key can come later if budgets need it.
  - **GitHub Models as the default.** #2109 first named it, but GitHub retired it on 2026-07-30,
    inference API included. OpenRouter takes its place: the same publisher-prefixed model ids, so
    `openai/gpt-4.1-mini` stays the default model, and one key reaches many publishers.
  - **Falling back to the recording on a broken key.** An operator would see canned answers and
    take them for the model's.
