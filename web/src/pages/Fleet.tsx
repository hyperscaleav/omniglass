import { For, Show, createMemo } from "solid-js";
import { useNavigate } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import Breadcrumb from "../components/Breadcrumb";
import HealthBadge from "../components/HealthBadge";
import BandCanvas from "../components/BandCanvas";
import ZoomLadder from "../components/ZoomLadder";
import FleetInspector from "../components/FleetInspector";
import { FLEET_VIEW_KEY, bandsOf, fleetView, holeOnlyBands, holesByRoot, type Band, type FleetView } from "../lib/fleet";
import { zoomChips } from "../lib/zoom";
import { entityLabel } from "../lib/entities";
import { describeError } from "../lib/format";

// The fleet zoom (#633): the whole fleet on one canvas. One band per root
// location, its systems as wrapping dot clusters, the holes drawn dashed.
//
// DOM owns all chrome (labels, chips, counts, holes, the ladder, the
// inspector); the canvas owns only the dot field, one canvas per band. Six of
// the eight acceptance criteria name text or a click target, and canvas text
// is invisible to the accessibility tree, to getByText, and to the keyboard,
// so the split is load-bearing rather than stylistic.
export default function Fleet() {
  const navigate = useNavigate();
  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));

  const bands = createMemo<Band[]>(() => {
    if (!view.data) return [];
    // System bands plus the roots that hold only holes: a site nobody has
    // commissioned is a band of gaps, not invisible.
    return [...bandsOf(view.data), ...holeOnlyBands(view.data)].sort((a, b) => a.label.localeCompare(b.label));
  });
  const holes = createMemo(() => (view.data ? holesByRoot(view.data) : new Map()));
  const chips = createMemo(() => (view.data ? zoomChips("fleet", {}, view.data) : []));

  return (
    <Page title="Fleet" subtitle="Explore your environment." breadcrumb={<Breadcrumb crumbs={[{ key: "fleet", label: "Fleet" }]} />}>
      <Show when={!view.isPending} fallback={<div class="skeleton h-32 w-full" />}>
        <Show
          when={!view.isError}
          fallback={
            <div role="alert" class="alert alert-error alert-soft text-sm">
              {describeError(view.error)}
            </div>
          }
        >
          <ZoomLadder chips={chips()} />
          <div class="flex gap-6">
            <div class="flex min-w-0 flex-1 flex-col gap-5">
              <For each={bands()}>{(band) => <FleetBand band={band} view={view.data!} onOpen={(id) => navigate(`/locations/${encodeURIComponent(id)}?zoom=1`)} />}</For>
              <Show when={bands().length === 0}>
                <p class="text-sm text-base-content/60">Nothing in scope yet.</p>
              </Show>
            </div>
            <Show when={view.data}>{(v) => <FleetInspector view={v()} />}</Show>
          </div>
        </Show>
      </Show>
    </Page>
  );

  function FleetBand(props: { band: Band; view: FleetView; onOpen: (id: string) => void }) {
    const bandHoles = () => holes().get(props.band.key) ?? [];
    const counts = () => {
      const b = props.band;
      const parts = [
        b.systemCount === 1 ? "1 system" : `${b.systemCount} systems`,
        b.componentCount === 1 ? "1 component" : `${b.componentCount} components`,
      ];
      if (b.depth > 0) parts.push(b.depth === 1 ? "1 level" : `${b.depth} levels`);
      return parts.join(", ");
    };
    return (
      <section data-testid={`band-${props.band.key}`} class="flex gap-4">
        <div class="w-52 flex-none">
          {/* The label column is a plain keyboard-reachable button (no btn
              class: it is a heading that navigates, not a form action). The
              chip renders the location's RECORDED verdict, the same row its
              detail page reads, so the two can never disagree one click
              apart. */}
          <button
            type="button"
            class="block w-full cursor-pointer rounded-lg p-1 text-left hover:bg-base-content/5"
            onClick={() => props.onOpen(props.band.key)}
          >
            <div class="flex items-center gap-2">
              <HealthBadge verdict={props.band.recordedVerdict ?? undefined} size="xs" />
              <span class="truncate font-medium">{props.band.label}</span>
            </div>
            <div class="mt-1 flex items-center gap-2">
              <Show when={props.band.sublabel}>
                <span class="text-[10px] uppercase tracking-wider text-base-content/50">{props.band.sublabel}</span>
              </Show>
            </div>
            <div class="mt-1 text-xs text-base-content/60">{counts()}</div>
          </button>
        </div>
        <div class="min-w-0 flex-1">
          <BandCanvas clusters={props.band.clusters} ariaLabel={`${props.band.label}: ${counts()}`} />
          <Show when={bandHoles().length > 0}>
            <div class="mt-2 flex flex-wrap gap-2">
              <For each={bandHoles()}>
                {(hole) => (
                  <div class="rounded-md border border-dashed border-base-content/25 px-2 py-1 text-xs text-base-content/50">
                    {entityLabel(hole)}
                  </div>
                )}
              </For>
            </div>
          </Show>
        </div>
      </section>
    );
  }
}
