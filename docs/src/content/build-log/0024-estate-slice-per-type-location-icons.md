---
title: "Estate slice: per-type location icons"
date: 2026-07-07
refs: []
form: bullet
---

- **Estate slice: per-type location icons.** `location_type` gains an `icon` column (a glyph key,
  seeded `landmark` / `building` / `layers` / `door-open` for the four official types, default
  `map-pin`), projected on `GET /types/location`. The console resolves the key to an SVG and renders
  it as the leading glyph on every location in the tree, tinted the same hue as the type badge, so a
  campus reads differently from a building at a glance. The glyph rides a new reusable `leadIcon` slot
  on the shared `ListView`, and `resolveIcon` falls back to `map-pin` for a key the console does not
  know, so the API can introduce a type icon without a coordinated release. Proven by a storage
  round-trip (the icon survives upsert and idempotent update), a seed test (the four shipped keys), a
  real-binary API test (the icon travels the wire), and a client unit (the resolver and its fallback).
  Deferred: per-location icon override, an operator icon picker (needs a `location_type` write
  surface), and server-side key validation.
