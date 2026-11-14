---
title: "A chart deployment keeps its encryption key and its unlanded telemetry (#1286)"
date: 2026-11-14
refs: [1286, 1287]
---

The Helm chart passed the server no encryption key and mounted no volume, so every pod restart
generated a new key in the container's writable layer (every stored secret unreadable after it) and
the embedded JetStream store sat in a temporary directory (a batch the stream had acknowledged but
not yet landed was gone). The server now reads `OMNIGLASS_ENCRYPTION_KEY` from a Secret: an
operator's `encryptionKey.existingSecret` if set, else one the chart generates once, reuses on every
upgrade through `lookup`, and keeps on uninstall (`helm.sh/resource-policy: keep`); with neither the
chart refuses to render. The data directory and the JetStream store sit on a kept
PersistentVolumeClaim at `/var/lib/omniglass` (`persistence.enabled`, default on), the lab's layout,
under a `Recreate` strategy. `NOTES.txt` prints the key's backup command. Chart render tests (Helm's
own engine) pin the defaults, the supplied Secret winning, the refusal and the notes; an integration
test pins a secret sealed under an env-sourced key reading back after a fresh key load; and a kind
cluster read a secret back after a rollout restart, an upgrade, and an uninstall and reinstall.
