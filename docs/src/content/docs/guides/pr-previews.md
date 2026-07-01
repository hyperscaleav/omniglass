---
title: "PR preview environments"
description: "How a pull request gets a live, isolated preview of the console, and how it is torn down."
---

Any pull request can get its own live copy of the operator console, at a
predictable URL, to review a change against a running system instead of reading
the diff.

## Getting a preview

Add the **`run:preview`** label to the PR. Within a minute or two a full instance
comes up at:

```
https://og-pr-<number>.preview.hyperscaleav.com/web
```

A bot comment on the PR carries the link. It is gated by Cloudflare Access, so
you sign in with your reviewer identity first. Each push to the PR rolls the
preview forward to that commit.

Previews are **opt-in** (the label) to bound how many run at once, and only
same-repo branches get one: a fork PR has no published image, so label it only
after a maintainer has built one.

## What is inside

Each preview is a throwaway, isolated instance in its own `og-pr-<number>`
namespace: the single binary with the console embedded, plus a bundled ephemeral
Postgres (data does not survive a restart, which is the point). It runs the chart
from *this PR's* commit, so schema migrations and API changes in the PR are
exercised end to end. Sign in as the seeded owner (the bot comment and the
[Helm guide](/guides/helm/) cover the credential).

## Teardown

Remove the `run:preview` label, or close the PR, and the environment is pruned. A
reaper also sweeps any namespace left behind, so nothing lingers.

## How it works

The image and chart are built here (see [Container image](/guides/container-image/)
and [Deploying with Helm](/guides/helm/)); the per-PR wiring lives in the ops
repo. An Argo CD PullRequest generator watches labeled PRs and renders the chart
into a per-PR namespace, pinned to the commit's immutable image tag. Traffic
reaches it through a Cloudflare tunnel to an in-cluster ingress controller that
Host-routes `og-pr-<number>.preview.hyperscaleav.com` to that preview.
