import { For, Show, createMemo } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import { systemHealth, systemHealthKey } from "../lib/health";
import { SYSTEMS_KEY, listSystems } from "../lib/systems";
import { slotStrip } from "../lib/slot_strip";
import { locationIndex, type FleetView, type SystemCluster } from "../lib/fleet";
import { entityLabel } from "../lib/entities";

// One system on the location zoom (#635, design fidelity pass): compact,
// status-bordered, `type · standard` line, a slot strip (one square per slot
// the standard wants, filled squares in the occupant's state, empty ones
// outlined), and the gap line in the design's words. Everything on it is the
// server's own arithmetic read back through slotStrip; nothing is computed.
export default function SystemCard(props: { cluster: SystemCluster; view: FleetView; onOpen?: (systemId: string) => void }) {
  const health = useQuery(() => ({ queryKey: systemHealthKey(props.cluster.systemId), queryFn: () => systemHealth(props.cluster.systemId) }));
  const systems = useQuery(() => ({ queryKey: SYSTEMS_KEY, queryFn: listSystems }));

  const strip = createMemo(() => (health.data ? slotStrip(health.data) : undefined));
  // The where-line names the ROOM, not its type: seven systems in seven rooms
  // all render "Meeting Room" from the same rule (a name is unique within its
  // placement, and each room holds the first), so the room is what tells the
  // cards apart on a grid. The standard rides after it.
  const room = createMemo(() => {
    const r = props.cluster.locationId ? locationIndex(props.view).get(props.cluster.locationId) : undefined;
    const label = r ? entityLabel(r) : undefined;
    // A system named for its room says the room twice (the leaf's rule,
    // extended here): the room earns its line only when it adds a fact.
    return label === props.cluster.label ? undefined : label;
  });
  const standard = createMemo(() => (systems.data ?? []).find((s) => s.id === props.cluster.systemId)?.standard);
  const v = () => props.cluster.verdict;

  return (
    <button
      type="button"
      data-testid={`syscard-${props.cluster.systemId}`}
      class="flex w-40 flex-none cursor-pointer flex-col gap-1 rounded-md border p-2 text-left hover:border-primary/50"
      classList={{
        "border-error/50 bg-error/5": v() === "outage",
        "border-warning/40 bg-warning/5": v() === "degraded",
        "border-incomplete/45 bg-incomplete/5": v() === "incomplete",
        "border-base-content/10": v() === "healthy" || v() === null,
      }}
      onClick={() => props.onOpen?.(props.cluster.systemId)}
    >
      <div class="flex items-center gap-1.5">
        <span
          class="h-1.5 w-1.5 flex-none rounded-full"
          classList={{
            "bg-error": v() === "outage",
            "bg-warning": v() === "degraded",
            "bg-incomplete": v() === "incomplete",
            "bg-success": v() === "healthy",
            "bg-base-content/30": v() === null,
          }}
        />
        <span class="truncate text-xs font-semibold">{props.cluster.label}</span>
      </div>
      <Show when={room() || standard()}>
        <div class="flex min-w-0 items-baseline gap-1.5 text-[10px] leading-tight">
          <Show when={room()}>
            <span class="truncate text-base-content/60">{room()}</span>
          </Show>
          <Show when={standard()}>
            <span class="truncate font-mono text-[9px] text-base-content/35">{standard()}</span>
          </Show>
        </div>
      </Show>
      <Show when={strip()}>
        {(s) => (
          <>
            <div data-testid="slot-strip" class="flex min-h-2 flex-wrap gap-0.5">
              <For each={s().squares}>
                {(sq) => (
                  <span
                    class="h-2 w-2 rounded-[1px]"
                    classList={{
                      "bg-success": sq.kind === "filled",
                      "bg-error": sq.kind === "down",
                      "border border-dashed border-incomplete": sq.kind === "empty",
                    }}
                    title={`${sq.role}${sq.occupant ? `: ${sq.occupant}` : ""}`}
                  />
                )}
              </For>
            </div>
            <span
              class="text-[10px]"
              classList={{ "text-incomplete": s().empty > 0, "text-error": s().empty === 0 && s().down > 0, "text-base-content/40": s().empty === 0 && s().down === 0 }}
            >
              {s().note}
            </span>
          </>
        )}
      </Show>
    </button>
  );
}
