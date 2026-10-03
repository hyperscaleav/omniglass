import { For, Show } from "solid-js";
import HealthBadge from "./HealthBadge";
import { entityLabel, type Labelled } from "../lib/entities";
import type { FleetView, SystemCluster } from "../lib/fleet";

// FleetRows: the subtree at list density (#798). One row per system, verdict
// first, the same data the cards draw, so the toggle changes density and
// nothing else. A row is a button that opens its system full screen (the
// altitude rule, ADR-0129).
export default function FleetRows(props: { rows: SystemCluster[]; view: FleetView; onOpen: (systemId: string) => void }) {
  const roomOf = (c: SystemCluster) => {
    const r = c.locationId ? (props.view.locations ?? []).find((l) => l.id === c.locationId) : undefined;
    const label = r ? entityLabel(r as unknown as Labelled) : undefined;
    return label === c.label ? undefined : label;
  };
  const attention = (c: SystemCluster) => c.dots.filter((d) => d.verdict !== null && d.verdict !== "healthy").length;
  return (
    <div data-testid="fleet-rows" class="flex flex-col divide-y divide-base-300">
      <For each={props.rows}>
        {(c) => (
          <button
            type="button"
            class="flex w-full cursor-pointer items-center gap-3 px-4 py-2.5 text-left text-sm hover:bg-base-content/5"
            onClick={() => props.onOpen(c.systemId)}
          >
            <HealthBadge verdict={c.verdict ?? undefined} size="xs" />
            <span class="min-w-0 truncate font-medium">{c.label}</span>
            <Show when={roomOf(c)}>{(room) => <span class="min-w-0 truncate text-base-content/60">{room()}</span>}</Show>
            <span class="flex-1" />
            <Show when={attention(c) > 0}>
              <span class="text-xs text-warning">{attention(c)} down</span>
            </Show>
            <span class="tnum text-xs text-base-content/50">{c.dots.length === 1 ? "1 component" : `${c.dots.length} components`}</span>
          </button>
        )}
      </For>
      <Show when={props.rows.length === 0}>
        <p class="px-4 py-6 text-sm text-base-content/60">Nothing under this location yet.</p>
      </Show>
    </div>
  );
}
