import { For, Show, createMemo } from "solid-js";
import { useNavigate } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import HealthBadge from "./HealthBadge";
import { FLEET_VIEW_KEY, fleetView } from "../lib/fleet";
import { systemHealth, systemHealthKey } from "../lib/health";
import { STANDARDS_KEY, listStandards } from "../lib/standards";
import { SYSTEMS_KEY, listSystems } from "../lib/systems";
import { alarmRows, sinceOf } from "../lib/system_zoom";
import { slotStrip } from "../lib/slot_strip";
import { entityLabel } from "../lib/entities";
import { fmtTime } from "../lib/format";
import { durationText } from "../lib/timeline";

// SystemSummary (#872): one system at a place holding several, in brief. What
// it is (label, standard), whether it needs somebody (verdict, since-when,
// the why), its slot arithmetic only while something is missing, and how
// many components it holds. Choosing it opens the system's own view, where
// its place is the card of context.
//
// It takes the system's id, not its object, and reads the object itself: a
// fresh fleet read can hand back a new object for the same system, and a
// list iterating objects would rebuild the card (and refetch its reads) on
// every one, which is a request storm.
export default function SystemSummary(props: { systemId: string }) {
  const navigate = useNavigate();
  const now = Date.now();
  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const system = () => (view.data?.systems ?? []).find((x) => x.id === props.systemId);
  const health = useQuery(() => ({ queryKey: systemHealthKey(props.systemId), queryFn: () => systemHealth(props.systemId), staleTime: 30_000 }));
  const systems = useQuery(() => ({ queryKey: SYSTEMS_KEY, queryFn: listSystems }));
  const standards = useQuery(() => ({ queryKey: STANDARDS_KEY, queryFn: listStandards }));
  const standard = createMemo(() => {
    const h = (systems.data ?? []).find((s) => s.id === props.systemId)?.standard;
    const row = h ? (standards.data ?? []).find((s) => s.name === h) : undefined;
    return row ? entityLabel(row) : h ?? "";
  });
  const why = createMemo(() => (health.data && view.data ? alarmRows(health.data, view.data, props.systemId) : []));
  const strip = createMemo(() => (health.data ? slotStrip(health.data) : undefined));
  const count = () => (system()?.dots ?? []).length;
  return (
    <button
      type="button"
      data-testid={`system-summary-${props.systemId}`}
      class="card flex cursor-pointer flex-col gap-2 border border-base-300 bg-base-200 p-4 text-left text-sm hover:border-primary/50"
      onClick={() => navigate(`/systems/${props.systemId}`)}
    >
      <span class="flex flex-wrap items-center gap-x-3 gap-y-1">
        <span class="font-semibold">{system() ? entityLabel(system()!) : ""}</span>
        <HealthBadge verdict={system()?.verdict ?? undefined} size="sm" />
        <Show when={health.data && sinceOf(health.data, now)}>
          {(sc) => <span data-testid="summary-since" class="tabular-nums text-base-content/60">since {fmtTime(sc().ts)} · {durationText(sc().ms)}</span>}
        </Show>
      </span>
      <span class="flex flex-wrap items-center gap-x-2 text-base-content/60">
        <Show when={standard()}><span>{standard()}</span><span class="text-base-content/30">·</span></Show>
        <span class="tabular-nums">{count()} {count() === 1 ? "component" : "components"}</span>
        <Show when={strip() && strip()!.empty > 0}>
          <span class="text-base-content/30">·</span>
          <span class="tabular-nums text-incomplete">{strip()!.filled} of {strip()!.total} slots filled</span>
        </Show>
      </span>
      <For each={why()}>
        {(a) => (
          <span class="flex items-baseline gap-2 text-xs">
            <span class="badge badge-xs" classList={{ "badge-error badge-soft": a.severity === "critical", "badge-warning badge-soft": a.severity !== "critical" }}>{a.severity}</span>
            <span class="font-data text-base-content/80">{a.component}</span>
            <span class="min-w-0 flex-1 truncate text-base-content/60">{a.message}</span>
          </span>
        )}
      </For>
    </button>
  );
}
