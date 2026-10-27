---
name: assess-ui
description: "Use for any change to an operator-facing surface (anything under web/src), twice: before building, to plan the surface against the design system and its siblings; and at PR-ready, to prove it visually by capturing the state matrix against the real console, reading every capture against the graded rubric, and iterating until clean. Emits the visual-assessment block and the committed screenshots the ship-review's Visual line requires. A green vitest run is not visual evidence; this gate is."
---

# Assess a UI surface

A surface can pass every component test and still ship ugly: misaligned, inconsistent
with its siblings, invisible on the dark theme, or silent about its empty state. This
skill makes "looks right" a checked claim. Two halves: a **plan pass** before code (most
visual defects are decided in the first hour, not painted at the end) and a **proof
pass** at PR-ready. The authority on every rule cited here is the
[design system](../../../docs/src/content/docs/contributing/design-system.md); `solidjs`
and `kobalte` cover the reactivity and primitive mechanics.

## The plan pass (before writing the component)

1. **Read the design-system page and the surface's architecture page.** The docs are the
   spec; a surface that contradicts them is drift even when it looks fine.
2. **Name the siblings.** Open the two nearest existing surfaces (route and source file)
   and inherit their skeleton: the `Page` header, the card chrome, where actions sit,
   how the table or blade is shaped. Consistency is inherited structure, not resemblance.
   An inventory-class entity is a `ListConfig` over `ListShell` and nothing else (the
   `add-inventory-view` skill); a new surface *class* builds its own primitive first and
   never bends `ListShell`.
3. **List the primitives you will consume**, by name, before coding: `FieldRow` /
   `KVStacked` / `BladeField` for anything labelled, `IdentityCell` for identity,
   `PanelFooter` bindings for actions (a body that draws its own button row is a bug the
   rail-ownership test catches), `InfoTip` / `Eyebrow` for teaching, `Drawer` for forms.
   Buttons carry an intent class (`btn-action`, `btn-quiet`, `btn-danger`, `btn-warn`,
   `btn-ok`) and never a raw daisyUI color class (the style-guard test fails the build).
   Status pills are soft hues; a neutral state uses the grey-fill recipe, never
   `badge-neutral` (invisible on this theme; `badge-ghost` is restyled in `app.css` as the
   legible neutral chip, and healthy wears `badge-healthy`). Data renders in
   `font-data`.
4. **Write the state list before the code.** Minimum: loading, empty (it must teach and
   offer the create path), error (an alert with a retry, not a blank), populated with
   the seeded fleet, long values, and denied-permission where the surface gates
   affordances. Each state becomes a capture in the proof pass; a state you cannot name
   now is a blank screen an operator finds later.
5. **Know the guards you are walking into**: `style-guard`, `validation-guard` (no
   native `required`/`min`/`max`/`pattern`; rules are TypeScript), the
   identity-vocabulary and one-label-renderer scans, rail-ownership. Design with them,
   not around them.

## The proof pass (drive the real console)

**Bring it up.** Full fidelity is `make lab`: Postgres, the server with the embedded build
at `http://127.0.0.1:8080/web`, and a node collecting from the simulated fleet, boot seed
plus the dev fleet, login `dev`/`dev` (`make lab LAB_ARGS=--fresh` starts over from the
seed). While iterating, keep the lab up and run `cd web && npm run dev` on :5173 (Vite
proxies `/api` to the lab; no rebuild per change), but **PR captures come from the embedded build**,
per the PR template. Permission-variant states use the seeded users `operator`,
`viewer` (fleet-wide read-only, the one who sees a component and may act on nothing),
`viewer-hq`, `tech-east` (password `dev`; a location grant reaches no component, so the
last two never open one), or a per-user token from signing in as them
(`./bin/omniglass auth login --server http://127.0.0.1:8080 --username viewer --password dev`).

**Capture the matrix.** One capture per state on the plan list, at the house viewport
(1320x860 at 2x, what `shot.mjs` ships):

```bash
node web/e2e/shot.mjs "http://127.0.0.1:8080/web/<route>" out.png \
  --token "$(./bin/omniglass token dev)" \
  [--click SEL]... [--select "SEL||VALUE"]... [--type "SEL||TEXT"]... \
  [--hover SEL]... [--press KEY]... [--wait MS] [--full]
```

Interactive states (an open menu, a chosen option, an edit face) are driven with the
step flags, never faked. The console ships **dark-only** (`omniglass-dark`); do not
chase a light theme. An error state the live console cannot be walked into is exercised
in a component test instead, and the verdict says so. Until #845 lands a `--viewport`
flag, a bespoke (non-`ListShell`) surface proves its squeeze behavior with a
narrow-viewport `web/e2e` test rather than a capture.

**Read every capture.** Open each PNG and grade it. This is the step that exists because
skipping it is how ugly ships: the captures are evidence only if they are actually
looked at. The rubric, each line pass or fail with a word of evidence:

1. **Kinship.** Put the capture beside the sibling surface's. Same header pattern, same
   rails, same spacing rhythm, same card chrome. A new visual idiom is drift unless an
   ADR says otherwise.
2. **Hierarchy.** One primary action per view, wearing `btn-action`; eyebrow labels;
   the eye lands on the operator's question first, not on chrome.
3. **States.** Empty teaches and offers the action; loading holds the layout (no jump
   when data lands); error alerts and offers retry.
4. **Density and alignment.** Columns align; spacing is the shell's, with nothing
   cramped against a card edge; long names truncate by the Name-floor rule and the card
   scrolls sideways before the page does.
5. **Legibility on the dark theme.** Every pill and chip visible (the banned neutral
   badges render invisible here); muted and disabled text still reads; data in
   `font-data`.
6. **Affordances.** Focus visible on a tab walk; every icon button has an accessible
   name; tooltips reachable by keyboard; no interactive trigger inside a `<label>`.
7. **Pedagogy.** The surface teaches through `InfoTip` and `Eyebrow` hints (the
   learning-tool doctrine), and carries no inline explanatory prose (tooltips, not
   prose).
8. **Honesty.** The shots show the real seeded fleet, never mocked data, and the
   capture rendered the vendored faces (the fontguard aborts a capture in fallback
   metrics; a capture that aborted is not evidence).

**Iterate.** Anything failing is a fix, not a note. Fix, recapture, regrade; the pass is
clean only when every line passes on fresh captures.

## Grade the walkthrough stills (an operator flow)

A PR that adds or changes an operator flow demos it with a walkthrough (`/ship-slice` gate 10,
the console's half of the PR's demo). You cannot watch the video, so the **per-step stills are
the evidence**: `make demo ID=<id>` writes `web/e2e/walkthroughs/out/<id>/step-NN.png` (one per step, taken after the
step's action settles, with the cursor, highlight ring and caption bar drawn in) and
`poster.png`. Open every still in order, beside its caption in the script, and grade each line
pass or fail with a word of evidence:

1. **Each step lands.** The still shows the result its caption promises (the drawer open, the
   value typed, the row selected), not the state before it. A still that shows nothing new is
   a missing `wait` or a step that did nothing.
2. **The eye can follow.** The cursor and highlight ring sit on the element the caption names;
   the caption bar covers nothing the step is about (a result hidden under the bar is a failed
   step, fix the layout or the target).
3. **Captions teach.** Each caption says why the operator does the step, in the product's
   vocabulary (component, place, system), and fits the bar on one or two lines.
4. **The flow is the shortest honest path.** No detour, no repeated step, no step the operator
   would not take; the order is the order the console invites.
5. **The poster is the payoff.** `poster.png` is the moment the walkthrough builds to (the step
   marked `poster: true`), legible on its own as the PR comment's lead image.
6. **Every still passes the surface rubric above**, since each is a capture of the real
   console (kinship, hierarchy, legibility and the rest).

A failing line is a fix (to the surface or the script), then a re-record and a regrade. The
same stills are what the `run:demo` comment embeds, so a clean grade here is what the
architect reviews there.

## Interaction proof (pixels are not behavior)

- `make test-web` green: typecheck first (vitest never typechecks), then the component
  tests. New logic gets tests in the house shape (`@solidjs/testing-library`, the query
  cache seeded directly; `web/src/pages/Locations.test.tsx` is the worked example).
- A new route registers in `web/src/lib/routemanifest.ts`, which buys it the e2e smoke
  (`routes.spec.ts` walks the manifest and fails on any `pageerror` or `console.error`).
  A new operator flow extends `console.spec.ts`. Routes or flows changed means
  `make test-e2e` runs before the PR.

## Evidence into the PR

1. Final captures are named `<issue#>-<n>-<step>.png` and committed under
   `.github/screenshots/` (a subdirectory per issue when there are several), embedded by
   immutable commit SHA
   (`https://raw.githubusercontent.com/<owner>/<repo>/<sha>/.github/screenshots/...`).
   That is the default for an unattended session; `gh image` is the alternative only
   where a logged-in browser exists.
2. **The docs-shots ripple.** If the change moves pixels on any page the docs
   photograph, the zero-tolerance freshness gate fails the PR until `make docs-shots`
   re-runs and **both** renders are committed (the clean shots and the masked
   baselines). A new docs screenshot is a `screenshots:` frontmatter entry plus a
   `::screenshot{#id}` directive, never a hand-added image.
3. Paste the verdict block into the ship-review's Visual line:

```
VISUAL ASSESSMENT - <surface> (issue #N)
Captures:  <n> states at 1320x860, dark (the console ships dark-only)
Rubric:    kinship PASS (matches <sibling>) / hierarchy PASS / states PASS /
           density PASS / legibility PASS / affordances PASS (tab walk clean) /
           pedagogy PASS (hints: <fields>) / honesty PASS (seed fleet, fontguard green)
Walkthrough: <id>: <n> stills graded, lands / follow / captions / path / poster PASS;
           comment <run:demo comment link> | n/a (no operator flow changed)
First-pass failures fixed: <what the loop caught, one line each | none>
Interaction: make test-web green; e2e <ran: routes+flows | n/a>
Evidence:  <committed raw-SHA links>
```

The "first-pass failures fixed" line is not shame, it is the loop working; an assessment
that never fails a line was probably not read.
