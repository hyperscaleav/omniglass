---
title: "On the fleet canvas, health colour is exceptional and identity colour is the ground"
date: 2026-08-15
status: "Superseded"
summary: "A healthy dot on the fleet canvas wears its system's identity hue (the `.og-system-dot` OKLCH recipe over `hueFor`); incomplete, degraded and outage dots wear the semantic verdict colours; an unnarrowed verdict wears `--og-unknown`. Healthy is the wallpaper and the wallpaper is identity, so failures are the only status-coloured pixels. Diverges from the prototype, which had no per-system hue"
---

- **Date:** 2026-08-15 | **Status:** Superseded (2026-08-16, by the amendment below) | **Pages:** [ui](/architecture/ui/), [health](/architecture/health/)
- **Amendment (2026-08-16):** reversed for operator clarity, on the architect's review of the
  shipped canvas against the design it was built from. A dot's colour is its component's
  verdict and nothing else (one channel, one meaning; a healthy dot is green), and the
  system's verdict rides the **cluster outline**, neutral when healthy and tinted when not,
  which is what the design's per-system card border was. Identity hue was this decision's
  own invention, not the design's, and it made a colour mean two things. A side effect: the
  canvas raster is now deterministic per seed, so the screenshot gate no longer masks it.
  `hueFor` stays for the surfaces that use it as identity (list rows, health panels).
- **Decision:** a **healthy** component dot wears its system's identity hue, through the exact
  recipe the DOM's `.og-system-dot` already uses (`oklch(var(--tag-l) var(--tag-c) <hueFor>)`),
  so a canvas dot and a DOM dot can never drift. **Incomplete, degraded and outage** dots wear
  the semantic verdict colours (`--og-incomplete`, `--color-warning`, `--color-error`); a verdict
  the console cannot narrow wears `--og-unknown` and never a substituted word. `hueFor`'s
  reserved OKLCH bands already guarantee an identity hue cannot land inside a status band, which
  is the invariant that makes the mixture legible.
- **Context:** the slice inherited a contradiction: "coloured by state" beside "takes its hue
  from `system_color`", and the prototype resolved it a third way (no per-system hue at all).
  Ruled during definition true-up: a healthy fleet should read as identity-coloured wallpaper
  with the clusters telling themselves apart, and anything not healthy should be the only
  status-coloured pixels on screen, which is what makes a failure pop at fleet scale. The paint
  plan (`paintGroups`) pins the rule at the unit tier per group, not per pixel.
- **Tracked under** epic [#630](https://github.com/hyperscaleav/omniglass/issues/630), slice
  [#633](https://github.com/hyperscaleav/omniglass/issues/633).
