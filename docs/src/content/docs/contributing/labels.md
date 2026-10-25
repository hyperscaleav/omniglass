---
title: "Labels"
description: "The fixed label taxonomy: what is a native Issue Type, what is a Project field, and what stays a label."
---

Labels are a small, fixed set. Two whole categories that would otherwise be labels
are not labels here: they live in first-class GitHub fields instead, and labels are
reserved for the axes those fields do not cover.

## What is not a label

- **Kind of work** is a native **Issue Type** (`Task`, `Bug`, `Feature`, `Epic`), set on
  the issue itself, never a `type:*` label. It maps to the conventional-commit type the
  eventual PR carries: `Task` = ci/docs/chore/refactor/test/perf, `Bug` = fix,
  `Feature` = feat, `Epic` groups a body of work. One field, filterable, with no parallel
  label to drift out of sync.
- **Priority** is a single-select **field on the Omniglass Project board**
  (`High`/`Medium`/`Low`), never a `prio:*` label. It sorts and groups on the board, where
  planning actually happens.

Using a label for either would duplicate a field GitHub already models, and two systems
for one fact drift apart.

## What is a label

Everything left is one of two prefixed families. The prefix encodes the one distinction
that matters: does the label **describe** the issue, or does it **trigger** automation?

### `area:<subsystem>` describes

The subsystem a change touches, named to match the architecture glossary. The set grows as
subsystems land; today:

`area:foundation` `area:api` `area:storage` `area:auth` `area:collection` `area:expr`
`area:ui` `area:node` `area:health` `area:settings` `area:tags` `area:files`

Purely descriptive: it filters the issue list by subsystem. An issue can carry more than
one.

### `run:<task>` triggers

A label that makes CI *do something* when it is applied. The `run:` prefix is a two-way
promise: if a label runs automation it is named `run:<task>`, and if a label is named
`run:<task>` then applying it runs that task.

| Label | Applying it | Wired |
| --- | --- | --- |
| `run:preview` | spins up a live [PR preview environment](/guides/pr-previews/); removing it tears the preview down | yes |
| `run:demo` | records the PR's [demos](/contributing/demos/) in CI (its terminal demos and its walkthroughs) and upserts one comment listing every demo, each with a scrubbable video and what plays inline | yes |
| `run:build` | rebuilds the release binaries and container image for the PR head on demand | reserved |
| `run:test` | runs the full test suite on demand | reserved |

`run:preview` is live today (the [PR previews guide](/guides/pr-previews/) covers it, and
`preview-comment.yml` is the workflow behind it). So is `run:demo` (`demo.yml`). Applying it
records the PR's demos on the walkthrough stack, each with its own recorder: the tapes
(`scripts/demo/<id>.tape`, terminal demos) and walkthrough scripts it adds or changes; every tape
when it changes the terminal recorder, and every walkthrough when it changes anything else under
`web/`, since any console change can move a flow; every demo when it names none. It then commits
each passing demo's files under `<id>/` to the PR's own evidence ref, `refs/walkthroughs/pr-<n>`,
as a fresh tree (so a renamed or deleted demo leaves nothing behind) on top of the ref's last
commit, and keeps **one comment** on the PR listing every demo and reaching its files by that
commit's SHA: each demo leads with a **Watch (scrubbable)** link to its video through jsDelivr,
which serves it as video with range requests so the browser's player seeks, then what plays on the
PR page (a terminal demo's GIF; a walkthrough's animated preview, then its poster and its captioned
stills folded below). The comment also links the run's workflow artifact (which keeps a failed
demo's partial output) and names any demo that failed (a demo is a test, so a failure fails the
job; a recorder whose stack never came up is reported as that, not as every one of its demos
failing). Why a ref rather than a branch is in [demos](/contributing/demos/#show-it-on-the-pr).

`run:walkthrough` is `run:demo`'s earlier name, from when walkthroughs were the only demos. The
workflow still honors it, so a PR that carries it (or a re-run of a run it started) records as
before, and a PR's existing walkthrough comment is found and updated in place rather than joined by
a second one. It is not a second task: apply `run:demo`.

- **What it costs:** nothing on main. The evidence never touches the PR branch, so a squash-merge
  carries none of it, and a default clone does not fetch the ref. It does sit in the repository's
  object store on GitHub and in any mirror of every ref (a few MB per demo per run, video
  included), until `demo-prune.yml` deletes the ref 30 days after the PR closes.
- **The PR's checks:** untouched. The run pushes only the evidence ref, never the PR head, so there
  is nothing to pull after a run and no check left un-run on the head.

- **What it records:** the branch tip when the job runs, not the commit the label was applied
  at. The label stays on; re-run the job, or remove and re-add the label, to record the current
  head again, and the comment updates in place.
- **Merge conflicts:** GitHub runs no `pull_request` workflow on a PR with merge conflicts, so
  the label does nothing until they are resolved.
- **Forks:** fork PRs are skipped (their token cannot push).

`run:build` and `run:test` are reserved:
the expensive build and test tiers run on every PR today, and
[issue #78](https://github.com/hyperscaleav/omniglass/issues/78) tracks moving them behind
these labels so they run only on demand. Wire the workflow in the same PR that first relies
on a reserved label, rather than leaving a label that silently does nothing.

## The fixed set

That is the whole taxonomy: a native Issue Type, the Project priority field, `area:*`, and
`run:*`. There are no stock GitHub labels (`bug`, `enhancement`, `good first issue` and the
rest were removed) and no bare `type:*` or `prio:*` labels. Adding a label means either a
new subsystem (`area:*`) or a new automation task (`run:*`); anything else belongs in a
field.
