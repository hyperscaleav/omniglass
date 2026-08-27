---
name: docs-audit
description: The periodic fan-out audit of the docs corpus against the code, catching the semantic drift no lint can see (inverted claims, stale arguments, missing surfaces, lagging badges). Run quarterly or before a release tag; the output lands as GitHub issues, never as a file merged to main.
---

# Docs audit

The lint suite (`internal/docslint`, the CLI docs guard) catches identifier drift: a
retired noun, a phantom route or permission or column, a wrong flag, a badge that
disagrees with its fences. It cannot catch **semantic** drift: a claim the code inverted
(the audit's canonical case: "alarms never write samples" after a health slice made them),
an argument that stopped being true, a read surface a page forgot to mention, a
capability that outgrew its page. That class needs a periodic human-shaped read, and the
2026-07-30 audit, the run this skill encodes, proved the fan-out shape works: it produced
the drift-correction wave (#428), the lint suite (#429), the telemetry-integrity epic
(#430), and the corpus restructure (#434). Those issues are the surviving record of it;
the dated write-up it was read from is gone (#497), because a finding is tracked in an
issue or it is not tracked.

## When

Quarterly, or before a release tag, or after any wave of slices large enough that the
corpus might have quietly inverted a premise. Not per-PR: that is what the lints and the
`/ship-slice` neighbor check are for.

## The drift definition (use it verbatim)

Architecture pages describe the target model, so documented-but-unbuilt is fine when a
design fence or the badge says so. **"Drift" means one of:**

- the code does something different from the prose with no divergence note;
- a built capability sits behind a `Design` badge or inside a design fence;
- the prose uses vocabulary the code retired;
- a claim names a test, route, command, column, or file that does not exist.

(The last two classes are lint-covered now; the audit re-checks them only where a lint
is allowlisted, exempt, or fenced. The first two are the audit's real work.)

## Procedure: the fan-out

Pin a commit first (`git rev-parse HEAD`); every cluster audits the same tree.

Run one subagent per cluster, in parallel, each with the same posture: **read the page,
then read the code it describes, and believe the code.** The clusters:

1. **Fleet + catalogs**: core-entities, groups, tags, glossary vs `internal/storage`,
   the seed YAMLs, and the migrations.
2. **Values + config**: variables, settings, files, cascade vs `internal/storage`,
   `internal/settings`, `internal/blob`.
3. **Telemetry + collection**: properties, events, commands, collection, nodes,
   messaging, workers, storage vs `internal/bus`, `internal/node`,
   `internal/collection`, the proto.
4. **IAM + audit + api**: identity-access, audit, api vs `internal/api`,
   `internal/rbac`, `internal/scope`, `api/openapi.json`.
5. **UI + guides**: ui, the operator/admin guides vs `web/src` (load the `solidjs`
   skill before judging `web/src`) and the console nav.
6. **Meta + contributing**: the contributing tree, the spine, status, roadmap vs the
   Makefile, the workflows, and the skills.

Each subagent prompt names its pages, its code roots, the drift definition above, and
demands: a **verdict per page** (`clean` / `drifted` / `diverged-with-note`), each
finding with **file:line and the code evidence**, and an explicit "checked, clean" list
(so silence is a claim, not a gap). Sketch pages (the Design sidebar group) get a
lighter pass: only the built-today notes and any claim of existence are checked.

## The output shape (six sections, as the 2026-07-30 audit ran them)

1. **Where the implementation actually is**: the snapshot (page/badge census, table and
   route and command counts, the mature-vs-thin map).
2. **Cross-cutting drift themes**: the classes, not the instances.
3. **Page-by-page verdicts**: one row per page, verdict plus the one-line reason.
4. **P0: code issues found by the audit**: bugs the read surfaced (the audit found the
   exactly-once gap this way). Filed as `Bug` issues immediately.
5. **P1: drift to close, grouped into workable batches**: each batch one issue.
6. **Glaring items worth an explicit decision**: the forks only the architect can rule
   on, each phrased as a question with the alternatives.

## Landing it

The assembled artifact is a **triage source, not a document to merge**: it lands as

- one `Epic` issue per correction wave (the #428/#429/#433/#434 shape), with the
  sections as sub-issue seeds;
- `Bug` issues for every P0, filed the same day;
- decision-log entries only once the architect rules on section 6 items (use
  `/record-decision`).

Do not commit the audit file to `main`; if a working file helps, keep it out of the tree
or delete it in the same PR that dissolves it into issues. Compare against the previous
audit's issue set: a finding that survives two audits unfixed gets escalated on its
issue, not re-filed.
