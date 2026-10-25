---
title: Docs with everything
description: A feature is not done until the docs that teach it ship in the same PR.
---

Omniglass ships its documentation *as part of the product*. The docs are not an
afterthought in a separate wiki; they are Astro Starlight content under `docs/`, compiled
to a static site and published at docs.omniglass.hyperscaleav.com (and, in time, embedded
into the Go binary to serve at `/docs`). The architecture is
published ahead of the code, so the design is visible (and reviewable) before, or
alongside, the feature that implements it.

## The rule

**A feature is not done until the docs that teach it ship in the same PR.**

Concretely, a user-facing PR must do one of:

- change `docs/` to add or update the page(s) that explain the new behavior, or
- state a one-line justification in the PR body (pure refactor, internal-only change,
  etc.).

The docs-touched gate is enforced by the PR template checklist and the `/ship-slice`
pre-ship pass, not by a label-driven CI check (the
[label taxonomy](/contributing/labels/) is closed, and no workflow reads a docs label). The
justification path exists so the gate never blocks a genuine internal change, not as a
routine escape hatch.

A mechanical layer backs the human gate: the **docs lint suite** (`internal/docslint`, run
by `go test` and so by `make test` and CI) checks the hand-written docs against the code.
The suite covers vocabulary (retired identifiers must not appear in current-tense
prose, in the docs, the operator strings, and the generated OpenAPI descriptions),
the decision log's format, routes and permissions (both checked against the generated
spec, permissions in both directions), make targets, env vars, file paths, and
table.column references (against the generated schema facts), plus the badge-fence
coverage check and the docs-command-and-flag guard in `internal/cli`. The semantic
drift no lint can see gets the periodic `/docs-audit` pass (#429).

## What "the docs" means here

- **Architecture pages** (`/architecture/`) hold the model: the spine plus leaf
  documents, and the current decisions. Each official term is defined once in the
  [glossary](/architecture/glossary/) and not redefined in the leaves.
- **Guides** (`/guides/`) are how-to pages for someone *using* the product, split by
  audience: the **operator guide** (running the fleet from the console and the CLI), the
  **admin guide** (managing accounts, access, audit, and config), and **deployment**
  (standing the platform up). **A slice that ships or changes a user-facing surface ships or
  updates its guide in the same PR**, not just the architecture page, filed under the section
  that matches who does the task. The architecture page says how the surface is built; the
  guide says how to use it.
- **Concept and learning pages** teach a concept interactively (see
  [the learning-tool restriction](/contributing/learning-tool/)). When a feature introduces a concept
  an operator must understand, the teaching surface ships with it.
- **Contributor pages** (`/contributing/`) are this doctrine set.

So a feature that adds an operator surface usually touches two homes: the **architecture**
page (the model) and a **guide** (the how-to). A purely internal change touches neither and
states its one-line justification in the PR body.

## Status moves with the code

The architecture pages are written in the present tense as the **target design**, so build status is
carried *alongside* the prose, not woven into it, and keeping it current is part of docs-with-everything.
A slice that advances a page updates three surfaces in the **same PR**:

- the page's **status badge** moves to its new floor (`Design` to `Partial` to `Built`), which the live
  grid on [implementation status](/architecture/status/) reads directly, so the grid never lies;
- the [build log](/architecture/build-log/) gains the slice's entry, a new file of its own
  ([one file per entry](#the-logs-are-one-file-per-entry)); and
- if the shipped code **diverges** from a page's design, the page carries an inline note and a
  [decision-log](/architecture/decisions/) entry (an ADR) lands in the same PR.

Forward-looking intent that is not yet a slice lives in a GitHub epic and is indexed on the
[roadmap](/architecture/roadmap/); it is not written into a page as if built. This is the contract that
keeps the published design describing what exists: a built capability never sits behind a `Design` badge,
and a divergence is never silent.

### The logs are one file per entry

Nearly every PR adds a build-log entry and many add an ADR, so while each log was one page that every
PR appended to, any two open PRs conflicted at the same line. The [build log](/architecture/build-log/)
and the [decision log](/architecture/decisions/) are instead **fragment directories**: each entry is its
own file, the page holds none, and the docs build splices the entries into the page where a directive sits
(`::build-log`, `::decision-index`, `::decision-log`), rendered by `docs/src/plugins/fragments.mjs`. Two PRs
adding entries add two files.

- **A build-log entry** is `docs/src/content/build-log/<YYYY-MM-DD>-<slug>.md`, named for the day it lands
  (entries of one day order by slug). Its frontmatter is the heading and the references, and its body is the
  entry's prose:

  ```markdown
  ---
  title: "The thing that landed, said as an outcome (#1234)"
  date: 2026-10-24
  refs: [1234]
  ---

  What shipped, how it is proven, and what was deferred where.
  ```

- **A decision** is `docs/src/content/decisions/adr-NNNN-<slug>.md`, its frontmatter the index row (title,
  date, status, summary, and `refs`) and its body the entry; the index table is generated from the files.
  The `/record-decision` skill is the procedure. **An ADR's number is the number of the issue it was
  decided under** (`refs` names it): a decision made on issue #NNNN is ADR-NNNN, and a second one on the
  same issue ADR-NNNN.2. Issue numbers never collide, so two PRs recording decisions at once never compete
  for a number. ADR-0001 to ADR-0163 are the closed legacy range, numbered in sequence before the rule.
  The one clash left is two PRs both adding an issue's next suffix: the decisions lint names both files,
  and the one landing second runs `make adr-renumber FROM=NNNN.2 TO=NNNN.3` (adding `FILE=` once both are
  in the tree), which renames its file and rewrites every reference across the docs, code comments, and
  skills.

The frontmatter is a small line grammar (a double-quoted or single-quoted string, a list of integers like
`[1234]`, or a bare word), read identically by the docs build and by `internal/docslint`, which checks every
entry's shape and fails an entry written into a log page instead of its own file. An entry body carries
no headings of its own and no directive lines (`::screenshot`, `:::design`) outside a code fence: a
directive resolves against the page's frontmatter, and an entry has none. The adversarial-review
anti-pattern catalog follows the same pattern, one file per entry under the skill's `references/anti-patterns/`.

## The design fence

A badge is page-granular, and a page is not one thing: most architecture pages mix built
behavior with target design. The **design fence** marks the unbuilt part structurally, at
the section where it lives, instead of a hand-written "still design" sentence at the top
that rots when a slice ships:

```markdown
:::design[Target design, tracked in #434]
The prose in here describes something that does not exist yet.
:::
```

The fence renders as a visually distinct aside (dashed, purple, labeled), and its label
**must name the issue or ADR that tracks the gap**; a fence with neither fails the docs
build, so unbuilt prose always has an owner. To nest a normal aside inside a fence, give
the fence more colons (`::::design` around a `:::note`), which is remark-directive's
nesting rule.

The fence is also the boundary the [docs lint suite](#the-rule) keys on
(`internal/docslint`, the `Regions` split): the **vocabulary lint runs everywhere**,
fenced or not, because future design must still be written in current nouns, while the
**existence lints** (routes, permissions, tables, make targets, env vars, paths) run
only outside a fence, where prose claims to describe something that exists. Building a fenced section
means **deleting the fence in the same PR**, exactly like moving the badge.

## Screenshots are generated, not pasted

Screenshots embedded on a docs page are a **generated resource**, treated like the OpenAPI
spec or the CLI reference, never a static image dropped in by hand. A page declares what it
needs in `screenshots` frontmatter (the shot's `id`, the console `path`, its `alt`, and any
interaction `steps`), and embeds it in the prose with a directive:

```markdown
---
title: Secrets
screenshots:
  - id: secrets
    path: /web/secrets
    alt: "The Secrets directory: type badges, owner scope, and masked field previews."
---

::screenshot{#secrets}
```

That frontmatter is the **single source**: `make docs-shots` reads it from every page, drives
the real console (the same binary an operator runs, never a mock), and writes
`public/screenshots/<id>.png`; the directive renders the figure from the same entry. So the
capture list and the embed cannot drift, a `#id` with no frontmatter entry (or no captured
image) **fails the build**, and adding a screenshot is a frontmatter edit, not a code change.

Because the images track the live UI, they are refreshed like any generated artifact: a change
to an operator surface **re-runs `make docs-shots` and commits the new PNGs**. Each capture
writes **two renders of the same settled page**: the clean shot under
`docs/public/screenshots/`, every region live, which is what the docs embed (a masked history
strip teaches nothing); and its masked twin under `docs/screenshots/baseline/`, where the
regions a capture cannot render deterministically (a v7-uuid id subtext, a seed-time
timestamp, a history strip whose weights divide by the capture's own clock) are painted as
constant boxes. `make docs-shots-check` recaptures and diffs **the baselines only**, at zero
tolerance, the visual sibling of the `make gen` drift check: the pinned browser's masked
rasters are byte-stable, so any diff is a real, un-recaptured UI change. The old percentage
ceiling passed exactly the changes the gate exists to catch, a renamed label or a collapsed
rail repainting fewer pixels than seed jitter (#398, #623). Two caveats: the comparison counts
the pixels pixelmatch judges different at its default perceptual threshold, so a change
confined to near-identical dark tones can still score zero (#774), and a real UI change that
falls entirely inside a masked region is invisible to the gate, which is the price of masking
and the reason a mask names the smallest box that holds still around what moves.

### A mask covers a box that holds still, and says that it is there

A mask is painted over **the element the selector resolves to**, so what a `mask` entry
resolves to is the whole contract. A `text=/…/` selector resolves to the innermost element
carrying the text, the value's own `<span>`, and so does a `data-testid` hook placed on that
span, so the painted box is the size of the value. The console does not render a
capture-relative value at one width (the audit trail's `toLocaleString` leaves the hour bare,
`fmtTime` prints a month name, leaves the day bare and prints AM or PM, an age reads 9s or
17s), which means the same unchanged page paints a box seven pixels narrower at 9 o'clock than
at 11, and every recapture rewrites the baseline on a PR that changed no UI. So a mask over
text whose width can change paints a **box the text cannot move**, one of two ways:

- **Name a container the layout pins.** The table **cell**, by appending
  `>> xpath=ancestor::td[1]`, whose width its column descriptor pins (#773); or a whole row,
  when the values that follow the text on its line move with it (a system header's standard
  and size after its since-line). Check the container's height as well as its width: a
  wrapping row whose wrap depends on the value's width does not hold still.
- **Declare the box**, when the only container that holds still also holds what the shot
  exists to show (a since-line beside the verdict it qualifies, #1081):

  ```yaml
  mask:
    - selector: "[data-testid=blade-since]"
      width: 184
  ```

  The box is painted at the value's top-left, as tall as the value and `width` CSS pixels
  wide, whatever the value reads; it suits a value whose left edge stays put (not right-aligned
  or centered, which moves the left edge with the text) and which ends its line. Size it from
  the widest value a capture can render, measured in the console's own face, with some
  headroom, and say the measurement in the frontmatter comment. A value wider than its box
  fails the capture by name rather than leaving its tail live.

A mask over text of fixed width (a uuid in a monospace face) stays on the text, which is also
the only correct answer when the enclosing cell holds something the shot exists to show.

The fill is **deliberately visible** in the baseline: the console's own neutral surface, one
step lighter than the card behind it, so a human reading a diff artifact sees a block placed
over a value rather than a column that failed to render. The published docs never show the
fill; they embed the clean render.

Prove a mask holds by recapturing **with the clock moved**, never by capturing twice a few
minutes apart: two captures in the same hour of the same day are structurally blind to exactly
the jitter a mask exists to absorb. And prove it **exactly**: the gate's comparison counts the
pixels pixelmatch judges different at threshold 0.1 (#774), which scores a mask edge moving one
pixel over a dark surface as zero, so the gate alone passes a mask that moves. `DOCS_SHOTS_EXACT=1`
compares every pixel instead:

```bash
make docs-shots-check                                                # the gate
DOCS_SHOTS_EXACT=1 DOCS_SHOTS_TZ=Asia/Kolkata make docs-shots-check  # the proof: the clock moved, every pixel equal
```

The contract (the page's declared mask paints a box the value cannot move, leaves what the shot
shows live, and the raster survives the value changing) is pinned at the browser tier by
`web/e2e/shotmask.spec.ts`. It reads each mask from the page's own frontmatter and drives the
capture's own mask code (`web/e2e/shotmasklib.mjs`) against synthetic markup, changing the value
on demand, so it also covers the single-digit day a timezone shift cannot reach. A new mask over
a varying value adds its case there. Every capture logs what each mask resolved to
(`mask 59x td 190x37 <- text=/…/`, `mask  1x box 184x16 over span 157 <- [data-testid=blade-since]`),
so a mask that lands on the wrong element shows up in the capture output rather than in a raster
nobody diffs by hand: a selector mask whose box is as wide as its text is the tell.

## Every PR demos its feature

The docs teach a feature; a **demo** shows it working, on the PR itself, as a recording the
reviewer watches rather than a transcript they read. Every PR carries one: a **terminal demo** (a
VHS tape typed into a real terminal) for CLI and API behavior, a **walkthrough** (a Playwright
recording of the console) for an operator flow, both when it has both, and "n/a" only with a
reason in one clause (a test-only or docs-only change). The PR body's Demo section links the
comment the `run:demo` label posts, and `/ship-slice` holds a PR that needs a demo to it, as it
holds a UI PR to its screenshots. Writing, recording and reviewing either kind is
[demos](/contributing/demos/).

## Walkthroughs embed the same way

A page that teaches an operator flow can embed its **walkthrough** (the recorded, captioned flow a
PR carries as evidence) exactly as it embeds a screenshot: the id in `walkthroughs` frontmatter and
`::walkthrough{#id}` where it belongs. The page declares only the id; the title, captions, video
and stills come from the recording, copied into `docs/public/walkthroughs/<id>/` by
`make docs-walkthroughs`, so a re-recorded flow updates the docs by a copy, never by an edit, and
the embed plays the video with controls. Writing,
recording, reviewing and embedding one is [demos](/contributing/demos/#embed-a-walkthrough-in-the-docs).

## Style

- No em dashes. Use commas, colons, periods, or parentheses.
- No AI/assistant attribution.
- Write for someone learning the system, not someone who already built it. The same page
  serves the operator using the product and the contributor extending it.

## Publishing

The docs site builds in CI on every PR that touches `docs/**` (the `docs-build` workflow,
`.github/workflows/docs-build.yml`, path-filtered), so a broken `.mdx` page, a dead import,
or invalid frontmatter fails the PR that introduces it. The published site is
docs.omniglass.hyperscaleav.com; in time, the same content is embedded into the Go binary
to serve at `/docs`.
