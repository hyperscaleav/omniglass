---
title: "The key that seals every secret is the encryption key, and its old names are read for one release"
date: 2026-10-26
status: "Accepted"
summary: "The key-encryption key's variables are `OMNIGLASS_ENCRYPTION_KEY` and `OMNIGLASS_ENCRYPTION_KEY_FILE`, and no operator-facing string calls it a \"secret key\", which names a secret's own identifier. The old pair (`OMNIGLASS_SECRET_KEY`, `OMNIGLASS_SECRET_KEY_FILE`) is read for a deprecation window, only while neither new name is set, warning on every boot with the replacement; once a new name is set the old generation is ignored as a whole and a leftover is called out. The environment registry declares the rename (`Deprecated`) and the generated reference renders it. Removing the old names is a breaking change for a later release"
refs: [558, 825]
---

- **Date:** 2026-10-26 | **Status:** Accepted | **Pages:** [config, secrets, and variables](/architecture/variables/), [Helm](/guides/helm/)
- **Decision:** (#558, pre-tag hygiene for 0.1.0, #825.)
  1. **It is the encryption key.** The variables that supply the key every secret is sealed under
     are `OMNIGLASS_ENCRYPTION_KEY` (base64, 32 bytes) and `OMNIGLASS_ENCRYPTION_KEY_FILE` (a path
     to it), the boot log reads `encryption key loaded`, the fallback warning says it generated an
     encryption key, and a missing provider is `no encryption key provider configured`. A secret's
     own identifier keeps the word **Key**.
  2. **The old names are read for one deprecation window.** `OMNIGLASS_SECRET_KEY` and
     `OMNIGLASS_SECRET_KEY_FILE` are read only while neither new name is set, and each one set logs a
     warning naming its replacement on **every** boot, not once: a deprecation an operator can miss
     on one boot is one they will miss.
  3. **The new generation wins as a whole.** When either new name is set, both old names are
     ignored, whichever source each uses: a new key file outranks an old key variable, because an
     operator who has started the move meant the new value. A leftover old name warns that it is
     ignored, so it does not linger. Within a generation the variable still outranks the file.
  4. **The rename is declared once.** The environment registry carries `Deprecated` (the
     replacing variable) on each old name, a test holds it to a registered, current replacement in
     the same scope, and the generated env-var reference renders it, so the deployment guides teach
     the rename from the same declaration the binary reads.
- **Context:** "secret key" meant two things in shipped code: the identifier an operator gives a
  secret (`og_session`, labelled **Key** in the console) and the master key that encrypts every
  secret. The second is a credential whose compromise is total, so an operator who cannot tell it
  from the name of one secret is an ambiguity worth removing rather than documenting. The variable
  is how every real deployment supplies the key, so a rename that read only the new names would
  lock an operator out of their own encrypted data on upgrade; reading the old pair through a
  window, loudly, is what makes it safe to do before the first public tag rather than after. The
  fallback key's file under the data directory keeps its name (`secret.key`): it is not an
  operator-configured name, and renaming it would need a second migration path for a convenience
  that is not the deployment model.
