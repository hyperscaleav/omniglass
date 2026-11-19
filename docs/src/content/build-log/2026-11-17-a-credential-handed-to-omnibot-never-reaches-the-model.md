---
title: "A credential handed to omnibot never reaches the model (#2112)"
date: 2026-11-17
refs: [2112, 1166, 1161]
---

`omniglass omnibot` with no prompt now opens a chat, its loop keeping the conversation across turns,
and the chat's `/secret <name> <type> [location:<name>|component:<name>]` hands omnibot a credential
it can use but never read. It asks for the type's values with the terminal's echo off, creates the
secret with the person's own token (audited as theirs, with no agent), and tells the model only the
secret's name, type and owner; the model then binds it by name with `secret:bind`, audited as
omnibot's. `TestOmnibotSecretNeverReachesTheModel` replays that chat and checks every message the
model was sent, both output streams and a failed `/secret`'s error for the value, and the audit rows
for who did what; it failed before (there was no chat to type `/secret` in) and passes now.
