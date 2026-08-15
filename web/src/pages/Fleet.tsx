import { For, Show, createMemo } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import { FLEET_VIEW_KEY, bandsOf, fleetView, holesByRoot, type Band } from "../lib/fleet";
import { describeError } from "../lib/format";

// The fleet zoom (#633): the whole fleet on one canvas. One band per root
// location, its systems as wrapping dot clusters, the holes drawn dashed.
//
// DOM owns all chrome (labels, chips, counts, holes); the canvas owns only
// the dot field. Six of the eight acceptance criteria name text or a click
// target, and canvas text is invisible to the accessibility tree, to
// getByText, and to the keyboard, so the split is load-bearing rather than
// stylistic.
export default function Fleet() {
  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));

  const bands = createMemo<Band[]>(() => (view.data ? bandsOf(view.data) : []));

  return (
    <Page title="Fleet" subtitle="Explore your environment.">
      <Show when={!view.isPending} fallback={<div class="skeleton h-32 w-full" />}>
        <Show
          when={!view.isError}
          fallback={
            <div role="alert" class="alert alert-error alert-soft text-sm">
              {describeError(view.error)}
            </div>
          }
        >
          <div class="flex flex-col gap-4">
            <For each={bands()}>{(band) => <div data-band={band.key}>{band.label}</div>}</For>
            <Show when={view.data && holesByRoot(view.data).size === 0 && bands().length === 0}>
              <p class="text-sm text-base-content/60">Nothing in scope yet.</p>
            </Show>
          </div>
        </Show>
      </Show>
    </Page>
  );
}
