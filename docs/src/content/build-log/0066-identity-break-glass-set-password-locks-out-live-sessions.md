---
title: "Identity: break-glass `set-password` locks out live sessions"
date: 2026-07-13
refs: [198]
form: bullet
---

- **Identity: break-glass `set-password` locks out live sessions.** The direct-DB `omniglass set-password <user> <pw>` (the trusted recovery lane, and the **only** way to touch a compromised **owner**, whose credentials the API guards leave un-revocable) previously rotated only the password, so a stolen session cookie or API token kept working after the reset ([#198](https://github.com/hyperscaleav/omniglass/issues/198)). It now also revokes the target's live **sessions** (a break-glass is a lockout), and revokes its API **tokens** too with `--revoke-tokens` (kept by default, matching the password-change-keeps-tokens rule; a full compromise wants the flag). It reuses the `RevokeBearersByPurpose` primitive after `SetPassword`, resolving the principal by username; no API or console change (break-glass stays behind Postgres access). Proven by a real-Postgres CLI test: a live session stops authenticating after the reset, the token survives without the flag and is revoked with it, and the new password authenticates while the old one does not.
