import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Popover from "./ui/Popover";
import { Zap } from "./icons";
import { can, useMe } from "../lib/auth";
import { describeError } from "../lib/format";
import { pushToast } from "../lib/toast";
import { FAULT_GROUPS, FAULT_GROUP_LABEL, applyFaultSteps, faultActions, type FaultAction, type HeldFault } from "../lib/simfaults";
import { componentSimKey, getComponentSim, toImpairment, type SimComponent, type SimSurface } from "../lib/simulation";

// SimFaultMenu: the named ways to break a simulated device, and Restore (#2462),
// generated from what its profile can do (lib/simfaults). One button opens the
// menu; choosing an action runs it, with no JSON typed. It renders nothing for
// a caller who may not update the simulation (the routes' own permission): a
// viewer sees faults and cannot set them.
//
// The surface (which routes can be fired) is read when the menu opens, so 200
// rows on the Simulation page do not each read it; a host that already holds it
// (the drawer) passes it in. Pinned values and per-protocol refusal (#909) are
// the "pinned" group, empty until they land.
export default function SimFaultMenu(props: {
  componentId: string;
  sim: Pick<SimComponent, "impairment" | "faults"> | undefined;
  name?: string;
  surfaces?: readonly SimSurface[];
  // "button" (the default) is the labelled menu button; "inline" lays every
  // action out as buttons, for the drawer.
  layout?: "button" | "inline";
}): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const [open, setOpen] = createSignal(false);
  const [busy, setBusy] = createSignal<string | null>(null);
  const allowed = () => can(me.data, "simulation", "update");
  const inline = () => props.layout === "inline";
  // Read the surface only when it is wanted and not already held.
  const read = useQuery(() => ({
    queryKey: componentSimKey(props.componentId),
    queryFn: () => getComponentSim(props.componentId),
    enabled: allowed() && !props.surfaces && (open() || inline()),
    staleTime: 30_000,
    retry: false,
  }));
  const surfaces = () => props.surfaces ?? read.data?.surface ?? [];
  const held = (): HeldFault[] => (props.sim?.faults ?? []).map((f) => ({ path: f.path, value: f.value, default: f.default, since: f.since }));
  const actions = createMemo(() => faultActions(surfaces(), { impairment: toImpairment(props.sim?.impairment), faults: held() }));
  const grouped = createMemo(() =>
    FAULT_GROUPS.map((g) => ({ group: g, items: actions().filter((a) => a.group === g) })).filter((g) => g.items.length > 0),
  );

  async function run(a: FaultAction) {
    setOpen(false);
    setBusy(a.id);
    try {
      await applyFaultSteps(qc, props.componentId, a.steps);
    } catch (e) {
      pushToast({ tone: "error", title: `${a.label} failed`, body: () => <>{describeError(e)}</> });
    } finally {
      setBusy(null);
    }
  }

  const item = (a: FaultAction) => (
    <button
      type="button"
      class={inline() ? "btn btn-quiet btn-sm" : "w-full px-2 py-1.5 text-left text-[12px]"}
      classList={{ "btn-danger": inline() && a.group === "restore" }}
      title={a.hint}
      disabled={busy() !== null}
      data-testid="sim-fault-action"
      data-action={a.id}
      onClick={() => void run(a)}
    >
      {a.label}
    </button>
  );

  return (
    <Show when={allowed()}>
      <Show
        when={!inline()}
        fallback={
          <div class="flex flex-col gap-2.5" data-testid="sim-fault-actions">
            <Show when={!props.surfaces && read.isPending}>
              <div class="skeleton h-8 w-full" aria-busy="true" />
            </Show>
            <For each={grouped()}>
              {(g) => (
                <div class="flex flex-wrap items-center gap-1.5" data-group={g.group}>
                  <span class="eyebrow w-24 flex-none">{FAULT_GROUP_LABEL[g.group]}</span>
                  <For each={g.items}>{item}</For>
                </div>
              )}
            </For>
          </div>
        }
      >
        <Popover class="btn btn-quiet btn-xs gap-1.5"
          placement="bottom-end"
          open={open()}
          onOpenChange={setOpen}
          aria-label={`Break ${props.name ?? "this device"}`}
          title="Break this device on purpose, or restore it"
          contentClass="menu z-50 w-48 rounded-box border border-base-300 bg-base-100 p-1.5 shadow-2xl focus:outline-none"
          contentLabel="Break or restore"
          data-testid="sim-fault-menu"
          content={
            <>
              <Show when={!props.surfaces && read.isPending}>
                <div class="skeleton m-1 h-16" aria-busy="true" />
              </Show>
              <For each={grouped()}>
                {(g) => (
                  <>
                    <p class="menu-title px-2 pb-1 pt-1.5 text-[10.5px]">{FAULT_GROUP_LABEL[g.group]}</p>
                    <ul class="flex flex-col">
                      <For each={g.items}>{(a) => <li>{item(a)}</li>}</For>
                    </ul>
                  </>
                )}
              </For>
            </>
          }
        >
          <Zap size={13} />
          Break
        </Popover>
      </Show>
    </Show>
  );
}
