---
name: docs-diagram
description: Use when adding or editing a diagram in the omniglass docs site (docs/, Astro Starlight). Covers the D2 + astro-d2 setup (build-time inline SVG, ELK layout, the d2 binary prerequisite), the colors-live-in-CSS-not-source theming contract that makes diagrams follow Starlight's light/dark toggle, the semantic class to CSS-hook vocabulary, and the build/preview/screenshot loop.
---

# Docs diagram (D2)

Docs diagrams are authored in **D2** and rendered by **astro-d2** to **inline SVG at build
time**. No client-side JS, no layout shift, and the diagram is styled by the site's own CSS.
This replaces mermaid (which rendered client-side and could only theme off the OS preference).

## Prerequisite: the d2 binary

astro-d2 shells out to the `d2` CLI during `pnpm build`. Install it (this is a Go repo, so
`go install` is the natural path) and make sure it is on `PATH` when building:

```bash
go install oss.terrastruct.com/d2@latest   # lands in ~/go/bin
PATH="$HOME/go/bin:$PATH" pnpm build        # d2 must be resolvable here
```

Fallback when the binary is unavailable (some CI): set `experimental: { useD2js: true }` on
the integration to render through WebAssembly instead. ELK works under D2.js; TALA does not.

## Where it is wired

- `docs/astro.config.mjs`: the `d2({ layout: 'elk', inline: true, theme: { default: '0', dark: '200' } })`
  integration. `inline: true` is load-bearing: it embeds the SVG in the page so site CSS can
  theme it. ELK matches the layout heritage from mermaid; orthogonal routing reads best for
  architecture diagrams.
- `docs/src/styles/custom.css`: the `.d2-svg` rules that color the diagrams from brand tokens.
- `docs/public/d2/`: generated SVGs, **gitignored**. Source of truth is the D2 in the Markdown.

## The theming contract (the one rule that matters)

**Never put colors in the D2 source.** Two reasons:

1. D2 rejects `var()` and `currentColor` (literal named/hex/gradient only), so the source
   cannot reference the site palette.
2. A baked fill only switches on `@media (prefers-color-scheme)`, i.e. the OS preference, so
   it ignores Starlight's manual light/dark toggle.

Instead: the D2 source carries **structure and semantic classes only**; `custom.css` owns the
colors, pulling the `--sl-color-*` brand tokens that already flip on `[data-theme]`. A CSS rule
beats D2's presentation-attribute fill with no `!important` needed, so the diagram tracks the
toggle and stays on palette in both modes. Verify both modes (see the loop below).

### The class to CSS-hook vocabulary

Assign these classes in the D2 source; the matching rules already live in `custom.css`:

| D2 class | Role | CSS hook | Light/dark via |
| --- | --- | --- | --- |
| (canvas) | diagram background | `.d2-svg > rect:first-of-type` | `--sl-color-bg` |
| `node` | standard box | `.d2-svg .node > .shape > rect` | `--sl-color-accent-low` fill, `--sl-color-accent` stroke |
| `key` | highlighted box | `.d2-svg .key > .shape > rect` | `--sl-color-accent` fill, `--sl-color-accent-high` stroke |
| (edges) | connections | `.d2-svg path.connection` | `--sl-color-accent` stroke |
| (labels) | node + edge text | `.d2-svg .node > text`, `.key > text`, `text.text-italic` | `--sl-color-text` / `--sl-color-accent-high` |

The **canvas background is the easy one to miss**: D2 paints a full-size rect (`fill-N7`, white
in the light theme) that only darkens under `@media (prefers-color-scheme)`, so without the rule
above the toggle leaves a white box behind a dark page. `:first-of-type` is deliberate: it beats
D2's `.fill-N7` rule on specificity (0,2,1 vs 0,2,0). Mind specificity generally: an override has
to outweigh D2's `.<id> .fill-Nx` rules, so include enough of the path (the `.shape > rect` depth
already does for nodes).

Adding a new shape role (a container, a second highlight) is two steps: assign a new class in
the D2 source **and** add the matching `.d2-svg .<class> > .shape > rect` rule to `custom.css`.
A class with no CSS rule renders in D2's raw theme color and will not follow the toggle. After any
diagram change, **toggle light/dark in the preview** and confirm the canvas, boxes, and text all
move together; that is the check that catches a missing or under-specific hook.

## Authoring a diagram

A fenced ` ```d2 ` block in any `docs/src/content/docs/**/*.md`. Structure only, classes for
color:

````markdown
```d2
direction: right

classes: {
  node: { style.border-radius: 8 }
  key: { style: { border-radius: 8; bold: true } }
}

gear: gear { class: node }
datapoint: "datapoint\ncanonical signal" { class: key }   # \n for multi-line labels
gear -> datapoint: collect (node + edge parse)
action -> gear: command { style.stroke-dash: 4 }           # dashed edge
config -- datapoint: drift { style.stroke-dash: 4 }        # undirected
```
````

- Layout defaults to ELK from the config; override per diagram with fence meta (` ```d2 layout="dagre" `)
  only when a diagram lays out better another way. Do not depend on `tala` (paid, absent in CI).
- Use D2 **containers** for nesting (`sys: { a; b }`) rather than faking hierarchy; this is a
  reason to prefer D2 over mermaid subgraphs.
- Keep labels terse; the diagram teaches, it is not a paragraph.

## The loop

1. Edit the ` ```d2 ` block (and `custom.css` if you introduced a new class).
2. `PATH="$HOME/go/bin:$PATH" pnpm build` (must be green; a D2 syntax error fails the build).
3. `pnpm preview`, open the page.
4. **Verify both themes.** Toggle light/dark and confirm the diagram recolors with the page and
   stays legible. This is the step that catches a missing CSS hook or a color hardcoded in source.

Validate locally; do not lean on CI. Diagrams ship in the same PR as the docs they teach
(doctrine 3, docs with everything).
