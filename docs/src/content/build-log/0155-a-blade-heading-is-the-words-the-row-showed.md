---
title: "A blade heading is the words the row showed"
date: 2026-08-04
refs: [581]
form: bullet
---

- **A blade heading is the words the row showed** ([#581](https://github.com/hyperscaleav/omniglass/issues/581)).
  The third heading bug in the same family, and the one that made the rule structural. Types resolved
  its row correctly and tracked it correctly, and then read the wrong field off it: the name where its
  list showed the display name, so clicking "Machine hall" opened a blade headed `server-room`. The two
  existing guards both passed, because neither is about which field a correctly resolved row is read
  for.

  No regex catches "wrong field", so the third check is membership: a blade title renders through
  `BladeTitle` or is named in the guard with a reason. Writing that list found two more hand-rolled
  headings a manual sweep had missed (the node blade and the effective-property blade), which is the
  argument for the check in one line. The named exceptions are the four entities that carry no display
  name at all, where the name IS the operator-facing string: a secret, a variable, a tag, an interface.
  Verifying that claim corrected it: `display_name` on `SecretType` had been read as `display_name` on
  `Secret`, and a secret has none.
