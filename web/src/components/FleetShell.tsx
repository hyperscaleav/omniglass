import { Show, type Accessor, type JSX } from "solid-js";
import { useSearchParams } from "@solidjs/router";
import Button from "./Button";
import ListShell from "./ListShell";
import { Grid, Rows } from "./icons";
import type { Chip, FilterKey } from "../lib/predicate";
import { countsLine, type TileSpec } from "../lib/fleet_tiles";
import type { SystemCluster } from "../lib/fleet";

// The workspaces' shared frame (#630): the one counts line (#826, what the
// retired summary rail said with the zeros left out) over a card holding the
// workspace's own body. A workspace with rows to filter (a location's systems)
// passes them with its filter keys and gets the console's ListShell filter bar
// and a need-attention quick filter on the counts line; one with nothing to
// filter (a system, a component) passes neither and gets the bare card.

const NO_CHIPS: Chip[] = [];

export default function FleetShell(props: {
  tiles: TileSpec | undefined;
  rows?: SystemCluster[];
  filterKeys?: FilterKey<SystemCluster>[];
  chips?: Accessor<Chip[]>;
  onChips?: (chips: Chip[]) => void;
  placeholder?: string;
  // A workspace's own header line inside the card, above its body (a system's
  // verdict and slot count, say). Renders where the filter bar would when
  // there is nothing to filter; above the body when there is both.
  header?: JSX.Element;
  // The density toggle's list face (#798, ADR-0129: tables survive as a
  // list-density toggle). When set, the shell offers cards/list buttons and
  // `?view=list` swaps the whole body (header, filter bar, cards) for this
  // face; the view is a URL fact, so the address deep-links.
  list?: JSX.Element;
  children: JSX.Element;
}) {
  const [search, setSearch] = useSearchParams();
  const listMode = () => props.list != null && search.view === "list";
  const viewToggle = () => (
    <div data-testid="view-toggle" class="join flex-none">
      <Button square icon={Grid} title="Cards view" label="Cards view" class="join-item" intent={listMode() ? "quiet" : "action"} onClick={() => setSearch({ view: undefined })} />
      <Button square icon={Rows} title="List view" label="List view" class="join-item" intent={listMode() ? "action" : "quiet"} onClick={() => setSearch({ view: "list" })} />
    </div>
  );
  const filterKeys = () => props.filterKeys ?? [];
  const chips = () => props.chips?.() ?? NO_CHIPS;
  const facetActive = (v: string) => chips().some((c) => c.key === "verdict" && c.values.includes(v));

  const ATTENTION = ["outage", "degraded", "incomplete"];
  const attentionOn = () => ATTENTION.some(facetActive) && !facetActive("healthy");
  const toggleAttention = () => {
    const rest = chips().filter((c) => c.key !== "verdict");
    props.onChips?.(attentionOn() ? rest : [...rest, { key: "verdict", op: "eq", values: ATTENTION }]);
  };

  if (props.list != null) {
    return (
      <section class="fade-in flex flex-col gap-3.5">
        <Show when={listMode()} fallback={cardsFace(viewToggle)}>
          <div class="flex items-center justify-end">{viewToggle()}</div>
          {props.list}
        </Show>
      </section>
    );
  }
  return cardsFace();

  function cardsFace(toggle?: () => JSX.Element) {
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
                <Show when={idx > 0}><span class="text-base-content/30">{"\u00b7"}</span></Show>
                <Show when={/ needs? attention$/.test(part) && filterKeys().length > 0} fallback={<span class="tabular-nums">{part}</span>}>
                  <Button size="xs" intent={attentionOn() ? "action" : "quiet"} pressed={attentionOn()} onClick={toggleAttention} title="Filter to what needs attention">{part}</Button>
                </Show>
              </>
            ))}
            <span class="flex-1" />
            {toggle?.()}
          </div>
        )}
      </Show>
      <Show
        when={filterKeys().length > 0}
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
        <ListShell filterKeys={filterKeys()} rows={props.rows ?? []} chips={chips} onChips={(c) => props.onChips?.(c)} placeholder={props.placeholder}>
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
