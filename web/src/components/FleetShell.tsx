import { Show, type Accessor, type JSX } from "solid-js";
import { useSearchParams } from "@solidjs/router";
import Button from "./Button";
import ListShell from "./ListShell";
import { Grid, Rows } from "./icons";
import type { Chip, FilterKey } from "../lib/predicate";
import { countsLine, type TileSpec } from "../lib/fleet_tiles";
import type { SystemCluster } from "../lib/fleet";

// The fleet pages' shared frame (#630, ruled 2026-08-18): the same layout at
// every zoom, taken from the Locations list page. On top, a summary rail of
// badges that expands to a tile board (a verdict donut with a facet legend,
// count cards), every badge and legend row a filter toggle; below it the
// console's ListShell (filter bar + card) around the zoom's own body. The
// summary is FLEET-WIDE at every zoom: it is the standing "how is my fleet"
// while the body zooms. Right-side drawers exist only as detail blades.

export default function FleetShell(props: {
  storageKey: string;
  tiles: TileSpec | undefined;
  rows: SystemCluster[];
  filterKeys: FilterKey<SystemCluster>[];
  chips: Accessor<Chip[]>;
  onChips: (chips: Chip[]) => void;
  placeholder?: string;
  trailing?: JSX.Element;
  // A zoom's own header line inside the card, above its body (a system's
  // verdict and slot count, say). Renders where the filter bar would when a
  // zoom has nothing to filter; above the body when it has both.
  header?: JSX.Element;
  // The density toggle's list face (#798, ADR-0129: tables survive as a
  // list-density toggle). When set, the shell offers canvas/list buttons and
  // `?view=list` swaps the whole body (summary, filter bar, canvas) for this
  // face; the view is a URL fact, so the address deep-links. Leaving the list
  // clears `kind` with it (the fleet root's tab param has no meaning off it).
  list?: JSX.Element;
  children: JSX.Element;
}) {
  const [search, setSearch] = useSearchParams();
  const listMode = () => props.list != null && search.view === "list";
  const viewToggle = () => (
    <div data-testid="view-toggle" class="join flex-none">
      <Button square icon={Grid} title="Canvas view" label="Canvas view" class="join-item" intent={listMode() ? "quiet" : "action"} onClick={() => setSearch({ view: undefined, kind: undefined })} />
      <Button square icon={Rows} title="List view" label="List view" class="join-item" intent={listMode() ? "action" : "quiet"} onClick={() => setSearch({ view: "list" })} />
    </div>
  );
  // Facet plumbing over the one verdict chip, the same shape ListCtx gives
  // widgets on the inventory pages.
  const facetActive = (v: string) => props.chips().some((c) => c.key === "verdict" && c.values.includes(v));

  const ATTENTION = ["outage", "degraded", "incomplete"];
  const attentionOn = () => ATTENTION.some(facetActive) && !facetActive("healthy");
  const toggleAttention = () => {
    const rest = props.chips().filter((c) => c.key !== "verdict");
    props.onChips(attentionOn() ? rest : [...rest, { key: "verdict", op: "eq", values: ATTENTION }]);
  };


  if (props.list != null) {
    return (
      <section class="fade-in flex flex-col gap-3.5">
        <Show when={listMode()} fallback={canvasFace(viewToggle)}>
          <div class="flex items-center justify-end">{viewToggle()}</div>
          {props.list}
        </Show>
      </section>
    );
  }
  return canvasFace();

  function canvasFace(toggle?: () => JSX.Element) {
    return (
    <div class="flex flex-col gap-3.5">
      {/* The one counts line (#826): what the summary rail said, with the
          zeros left out. Need-attention stays a filter over the verdict
          facet when the page has rows to filter. */}
      <Show when={props.tiles}>
        {(t) => (
          <div data-testid="counts-line" class="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm text-base-content/70">
            {countsLine(t()).map((part, idx) => (
              <>
                <Show when={idx > 0}><span class="text-base-content/30">\u00b7</span></Show>
                <Show when={part.endsWith(" need attention") && props.filterKeys.length > 0} fallback={<span class="tabular-nums">{part}</span>}>
                  <Button size="xs" intent={attentionOn() ? "action" : "quiet"} onClick={toggleAttention} title="Filter to what needs attention">{part}</Button>
                </Show>
              </>
            ))}
            <span class="flex-1" />
            {toggle?.()}
          </div>
        )}
      </Show>
      <Show
        when={props.filterKeys.length > 0}
        fallback={
          <div class="og-stack flex flex-col">
            <div class="card overflow-hidden border border-base-300 bg-base-200 p-0">
              <Show when={props.header}>
                <div class="border-b border-base-300 px-4 py-3">{props.header}</div>
              </Show>
              {props.children}
            </div>
          </div>
        }
      >
        <ListShell filterKeys={props.filterKeys} rows={props.rows} chips={props.chips} onChips={props.onChips} trailing={props.trailing} placeholder={props.placeholder}>
          {() => (
            <>
              <Show when={props.header}>
                <div class="border-b border-base-300 px-4 py-3">{props.header}</div>
              </Show>
              {props.children}
            </>
          )}
        </ListShell>
      </Show>
    </div>
    );
  }
}
