---
title: "Settings read an env layer between the settings file and the console"
date: 2026-11-19
status: "Accepted"
summary: "An OMNIGLASS_SETTING__<NAMESPACE>__<KEY> variable feeds one setting at boot, parsed through the single-source Settings struct and fed to the resolver between the file level and the platform binding. The console (platform) still wins, since an admin's edit is the most specific thing a process sees. Profile-domain namespaces are refused with the variable named."
refs: [2223, 33, 41]
---

- **Date:** 2026-11-19 | **Status:** Accepted | **Pages:** [settings](/architecture/settings/), [Admin > Settings](/admin/settings/)
- **Decision:** (#2223.)
  1. **One variable, one key.** `OMNIGLASS_SETTING__<NAMESPACE>__<KEY>=value` sets the key in that
     namespace at a new `env` level. The double underscore marks the boundary between the two
     segments: a namespace never carries one, so the parser splits cleanly on it.
  2. **Validated against the declaration.** The value is coerced to the key's Go kind from the same
     reflected `Settings` struct the file layer validates against ([ADR-0041](/architecture/decisions/#adr-0041-settings-are-a-reflected-typed-struct-with-generated-client-and-server-validation)): a `[]string` takes a
     comma-separated list; a `bool` takes `true`/`false`/`1`/`0`; an int takes a base-10 int. A
     value that does not coerce fails the boot, naming the kind (`expected an int`), never quoting
     the value (it may be a bare secret).
  3. **Between the file and the console.** The resolver reads
     `default < file < env < platform (< group < user)`: the committed file is the baseline, the
     env the per-process override, the console the install-wide operator's decision. An admin edit
     still wins, so an operator trying a value in the console is never silently clamped by a
     container variable.
  4. **Profile domain refused.** A per-user namespace (`ui`, `keybindings`, any `profile`) has no
     process-wide meaning: a theme is not a server setting. The parser refuses the variable at
     boot, naming it; the rest still loads.
  5. **Rendered in the generated reference.** The env-var reference (`<ConfigVars />`) emits one
     row per settable key, so the reference stays complete by construction and the registry's
     drift gate still fails when the facts move.
- **Context:** The twelve-factor rung between the file image (GitOps, ConfigMap) and the DB console
  binding was missing: an install wanting to flip one value per environment without an edit to its
  file had to open the console or ship a different file image. The env layer closes that gap
  without widening the console's surface: everything already settable at the platform rung is
  settable here.
- **Not chosen:**
  - **Above `platform`.** A variable that silently overrides the console would hide an operator's
    decision behind a container's environment, with no console badge to discover the override.
    Keeping it under `platform` leaves the console the authoritative read.
  - **One JSON blob per namespace.** `OMNIGLASS_SETTINGS='{"retention":{"metric_days":30}}'` is one
    variable per namespace but hides the schema inside the string: a typo in a key does not fail
    the boot, it just stores nothing. The per-key form carries each path to the type system.
  - **A separate parser per kind.** The file layer and the env layer share the single-source
    `Settings` struct's reflection, so a new kind added to the struct is coerced by both from the
    same declaration (doctrine 5).
